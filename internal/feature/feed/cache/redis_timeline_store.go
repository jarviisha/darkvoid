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

const timelineKeyPrefix = "feed:tl"

func timelineKey(userID uuid.UUID) string {
	return fmt.Sprintf("%s:%s", timelineKeyPrefix, userID)
}

// RedisTimelineStore stores prepared feed timelines in Redis sorted sets.
type RedisTimelineStore struct {
	client   *pkgredis.Client
	maxItems int
	ttl      time.Duration
}

func NewRedisTimelineStore(client *pkgredis.Client, maxItems int, ttl time.Duration) *RedisTimelineStore {
	if maxItems <= 0 {
		maxItems = 1000
	}
	if ttl <= 0 {
		ttl = 7 * 24 * time.Hour
	}
	return &RedisTimelineStore{client: client, maxItems: maxItems, ttl: ttl}
}

func (s *RedisTimelineStore) AddPost(ctx context.Context, userID uuid.UUID, entry feed.TimelineEntry) error {
	return s.AddPostsBatch(ctx, userID, []feed.TimelineEntry{entry})
}

func (s *RedisTimelineStore) AddPostsBatch(ctx context.Context, userID uuid.UUID, entries []feed.TimelineEntry) error {
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

	pipe := s.client.Pipeline()
	pipe.ZAddArgs(ctx, key, redis.ZAddArgs{NX: true, Members: members})
	pipe.ZRemRangeByRank(ctx, key, 0, int64(-s.maxItems-1))
	pipe.Expire(ctx, key, s.ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		feed.ObserveRedisError(err)
		return fmt.Errorf("redis timeline add batch: %w", err)
	}
	return nil
}

func (s *RedisTimelineStore) ReadPage(ctx context.Context, userID uuid.UUID, after *feed.TimelinePosition, limit int) (*feed.TimelinePage, error) {
	if limit <= 0 {
		return &feed.TimelinePage{}, nil
	}

	max := "+inf"
	if after != nil {
		max = strconv.FormatInt(after.Score, 10)
	}
	rows, err := s.client.ZRevRangeByScoreWithScores(ctx, timelineKey(userID), &redis.ZRangeBy{
		Max:    max,
		Min:    "-inf",
		Offset: 0,
		Count:  int64(limit * 2),
	}).Result()
	if err != nil {
		feed.ObserveRedisError(err)
		return nil, fmt.Errorf("redis timeline read page: %w", err)
	}

	entries := make([]feed.TimelineEntry, 0, min(limit, len(rows)))
	for _, row := range rows {
		postID, parseErr := uuid.Parse(fmt.Sprint(row.Member))
		if parseErr != nil {
			continue
		}
		score := int64(row.Score)
		if after != nil {
			if score > after.Score {
				continue
			}
			if score == after.Score && postID.String() >= after.PostID {
				continue
			}
		}
		entries = append(entries, feed.TimelineEntry{PostID: postID, Score: score})
		if len(entries) == limit {
			break
		}
	}

	page := &feed.TimelinePage{Entries: entries}
	if len(entries) > 0 {
		last := entries[len(entries)-1]
		page.Last = &feed.TimelinePosition{Score: last.Score, PostID: last.PostID.String()}
	}
	return page, nil
}

func (s *RedisTimelineStore) Trim(ctx context.Context, userID uuid.UUID) error {
	if err := s.client.ZRemRangeByRank(ctx, timelineKey(userID), 0, int64(-s.maxItems-1)).Err(); err != nil {
		feed.ObserveRedisError(err)
		return fmt.Errorf("redis timeline trim: %w", err)
	}
	return nil
}

func (s *RedisTimelineStore) RemovePostBestEffort(ctx context.Context, userID uuid.UUID, postID uuid.UUID) error {
	if err := s.client.ZRem(ctx, timelineKey(userID), postID.String()).Err(); err != nil {
		feed.ObserveRedisError(err)
		return fmt.Errorf("redis timeline remove post: %w", err)
	}
	return nil
}
