package cache

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jarviisha/darkvoid/internal/feature/feed"
	pkgredis "github.com/jarviisha/darkvoid/pkg/redis"
	"github.com/redis/go-redis/v9"
)

// timelineKeyPrefix is versioned: v2 keys hold packed rank scores. Legacy
// unversioned keys held UnixMicro timestamps in an overlapping numeric range,
// so they must never be read or written again — they expire via their TTL.
const timelineKeyPrefix = "feed:tl:v2"

const replaceTimelineScript = `
local key = KEYS[1]
local cutoff = tonumber(ARGV[1])
local maxItems = tonumber(ARGV[2])
local ttlMillis = tonumber(ARGV[3])
local keep = {}
for i = 4, #ARGV, 2 do
  local member = ARGV[i]
  keep[member] = true
  redis.call('ZADD', key, ARGV[i + 1], member)
end
local current = redis.call('ZRANGE', key, 0, -1, 'WITHSCORES')
for i = 1, #current, 2 do
  local member = current[i]
  if not keep[member] then
    local writtenAt = redis.call('ZSCORE', KEYS[2], member)
    if not writtenAt or tonumber(writtenAt) < cutoff then
      redis.call('ZREM', key, member)
    end
  end
end
redis.call('ZREMRANGEBYRANK', key, 0, -maxItems - 1)
if redis.call('ZCARD', key) == 0 then
  redis.call('DEL', key)
else
  redis.call('PEXPIRE', key, ttlMillis)
end
return 1
`

func timelineKey(userID uuid.UUID) string {
	return fmt.Sprintf("%s:%s", timelineKeyPrefix, userID)
}

func timelineWritesKey(userID uuid.UUID) string {
	return fmt.Sprintf("%s:writes", timelineKey(userID))
}

// RedisTimelineStore stores prepared feed timelines in Redis sorted sets.
//
// The retention limits are read per write rather than captured at construction,
// so lowering them starts reclaiming memory on the next fanout instead of on the
// next restart. Note that a lowered TTL only applies to keys written after the
// change: Redis holds one expiry per key, and existing timelines keep the one
// they were last given until something writes to them again.
type RedisTimelineStore struct {
	client   *pkgredis.Client
	settings *feed.Settings
}

func NewRedisTimelineStore(client *pkgredis.Client, settings *feed.Settings) *RedisTimelineStore {
	return &RedisTimelineStore{client: client, settings: settings}
}

func (s *RedisTimelineStore) AddPost(ctx context.Context, userID uuid.UUID, entry feed.TimelineEntry) error {
	return s.writeBatch(ctx, userID, []feed.TimelineEntry{entry}, true)
}

// SetPostsBatch upserts entries, overwriting scores of existing members. It is
// the write path for background ranking (refresher / re-rank jobs).
func (s *RedisTimelineStore) SetPostsBatch(ctx context.Context, userID uuid.UUID, entries []feed.TimelineEntry) error {
	return s.writeBatch(ctx, userID, entries, false)
}

func (s *RedisTimelineStore) ReplacePosts(ctx context.Context, userID uuid.UUID, entries []feed.TimelineEntry, preserveAfter time.Time) error {
	cutoff := preserveAfter.UTC().UnixMilli()
	maxItems, ttl := s.settings.TimelineWriteLimits()
	args := make([]any, 0, 3+len(entries)*2)
	args = append(args, cutoff, maxItems, ttl.Milliseconds())
	for _, entry := range entries {
		args = append(args, entry.PostID.String(), entry.Score)
	}
	if err := s.client.Eval(ctx, replaceTimelineScript, []string{timelineKey(userID), timelineWritesKey(userID)}, args...).Err(); err != nil {
		feed.ObserveRedisError(err)
		return fmt.Errorf("redis timeline replace posts: %w", err)
	}
	return nil
}

// writeBatch writes entries (ZADD NX when nx — never downgrading an existing
// score — plain upsert otherwise), trims to maxItems, and refreshes the TTL.
func (s *RedisTimelineStore) writeBatch(ctx context.Context, userID uuid.UUID, entries []feed.TimelineEntry, nx bool) error {
	if len(entries) == 0 {
		return nil
	}

	key := timelineKey(userID)
	members := make([]redis.Z, 0, len(entries))
	for _, entry := range entries {
		members = append(members, redis.Z{
			Score:  float64(entry.Score),
			Member: entry.PostID.String(),
		})
	}

	maxItems, ttl := s.settings.TimelineWriteLimits()
	// AddPost writes the timeline member and its fanout-write marker in one
	// Redis transaction. ReplacePosts is a Lua script, so it observes either
	// both writes or neither and cannot delete an in-flight fanout between them.
	pipe := s.client.TxPipeline()
	pipe.ZAddArgs(ctx, key, redis.ZAddArgs{NX: nx, Members: members})
	pipe.ZRemRangeByRank(ctx, key, 0, int64(-maxItems-1))
	pipe.Expire(ctx, key, ttl)
	if nx {
		writeMarkers := make([]redis.Z, 0, len(entries))
		writtenAt := float64(time.Now().UTC().UnixMilli())
		for _, entry := range entries {
			writeMarkers = append(writeMarkers, redis.Z{Score: writtenAt, Member: entry.PostID.String()})
		}
		writesKey := timelineWritesKey(userID)
		pipe.ZAdd(ctx, writesKey, writeMarkers...)
		pipe.ZRemRangeByRank(ctx, writesKey, 0, int64(-maxItems-1))
		pipe.Expire(ctx, writesKey, ttl)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		feed.ObserveRedisError(err)
		return fmt.Errorf("redis timeline write batch: %w", err)
	}
	return nil
}

func (s *RedisTimelineStore) ReadPage(ctx context.Context, userID uuid.UUID, after *feed.TimelinePosition, limit int) (*feed.TimelinePage, error) {
	if limit <= 0 {
		return &feed.TimelinePage{}, nil
	}

	maxScore := "+inf"
	if after != nil {
		maxScore = strconv.FormatInt(after.Score, 10)
	}
	want := limit + 1
	chunkSize := max(limit*2, 64)
	entries := make([]feed.TimelineEntry, 0, want)
	var offset int64
	for len(entries) < want {
		rows, err := s.client.ZRevRangeByScoreWithScores(ctx, timelineKey(userID), &redis.ZRangeBy{
			Max: maxScore, Min: "-inf", Offset: offset, Count: int64(chunkSize),
		}).Result()
		if err != nil {
			feed.ObserveRedisError(err)
			return nil, fmt.Errorf("redis timeline read page: %w", err)
		}
		offset += int64(len(rows))
		for _, row := range rows {
			postID, parseErr := uuid.Parse(fmt.Sprint(row.Member))
			if parseErr != nil {
				continue
			}
			score := int64(row.Score)
			if after != nil && score == after.Score && postID.String() >= after.PostID {
				continue
			}
			entries = append(entries, feed.TimelineEntry{PostID: postID, Score: score})
			if len(entries) == want {
				break
			}
		}
		if len(rows) < chunkSize {
			break
		}
	}

	page := &feed.TimelinePage{HasMore: len(entries) > limit}
	if page.HasMore {
		entries = entries[:limit]
	}
	page.Entries = entries
	if len(entries) > 0 {
		last := entries[len(entries)-1]
		page.Last = &feed.TimelinePosition{Score: last.Score, PostID: last.PostID.String()}
	}
	return page, nil
}

func (s *RedisTimelineStore) Trim(ctx context.Context, userID uuid.UUID) error {
	maxItems, _ := s.settings.TimelineWriteLimits()
	pipe := s.client.Pipeline()
	pipe.ZRemRangeByRank(ctx, timelineKey(userID), 0, int64(-maxItems-1))
	pipe.ZRemRangeByRank(ctx, timelineWritesKey(userID), 0, int64(-maxItems-1))
	if _, err := pipe.Exec(ctx); err != nil {
		feed.ObserveRedisError(err)
		return fmt.Errorf("redis timeline trim: %w", err)
	}
	return nil
}

func (s *RedisTimelineStore) RemovePostBestEffort(ctx context.Context, userID uuid.UUID, postID uuid.UUID) error {
	pipe := s.client.Pipeline()
	pipe.ZRem(ctx, timelineKey(userID), postID.String())
	pipe.ZRem(ctx, timelineWritesKey(userID), postID.String())
	if _, err := pipe.Exec(ctx); err != nil {
		feed.ObserveRedisError(err)
		return fmt.Errorf("redis timeline remove post: %w", err)
	}
	return nil
}
