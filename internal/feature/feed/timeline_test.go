package feed

import (
	"testing"
	"time"
)

func TestPackTimelineScore_ClampsToNonNegative(t *testing.T) {
	pre2020 := time.Date(2019, 6, 1, 0, 0, 0, 0, time.UTC)
	if got := PackTimelineScore(-5, pre2020); got != 0 {
		t.Fatalf("pack(negative rank, pre-epoch time) = %d, want 0", got)
	}
	if got := PackTimelineScore(30, pre2020); got < 0 || got&0xFFFFFFFF != 0 {
		t.Fatalf("pack(30, pre-epoch time) = %d, want non-negative with zero ts component", got)
	}
}

func TestPackTimelineScore_Float64RoundTripExact(t *testing.T) {
	far := time.Date(2200, 1, 1, 0, 0, 0, 0, time.UTC) // clamps ts to 2^32-1
	max := PackTimelineScore(1e12, far)                // clamps rank to bucket max
	wantMax := (int64(1)<<20-1)<<32 | (int64(1)<<32 - 1)
	if max != wantMax {
		t.Fatalf("max packed = %d, want %d", max, wantMax)
	}
	if max >= 1<<53 {
		t.Fatalf("max packed = %d, must stay below 2^53 for exact float64 round-trip", max)
	}
	if int64(float64(max)) != max {
		t.Fatalf("float64 round-trip lost precision: %d != %d", int64(float64(max)), max)
	}
}

func TestPackTimelineScore_RankMajorOrdering(t *testing.T) {
	oldTime := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)
	newTime := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	// A full 0.001 rank step must dominate any timestamp difference.
	if PackTimelineScore(10.001, oldTime) <= PackTimelineScore(10.0, newTime) {
		t.Fatal("higher rank bucket must outrank any newer timestamp")
	}
}

func TestPackTimelineScore_TimeMinorWithinBucket(t *testing.T) {
	base := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	earlier := PackTimelineScore(30, base)
	later := PackTimelineScore(30, base.Add(time.Second))
	if later <= earlier {
		t.Fatal("within one rank bucket, newer createdAt must score higher")
	}
	subSecond := PackTimelineScore(30, base.Add(500*time.Millisecond))
	if subSecond != earlier {
		t.Fatal("sub-second drift must collapse to the same score (postID breaks the tie)")
	}
}

func TestPackTimelineScore_SubResolutionTieSharesBucket(t *testing.T) {
	at := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	if PackTimelineScore(10.0, at) != PackTimelineScore(10.0004, at) {
		t.Fatal("ranks closer than the 0.001 resolution should share a bucket")
	}
}

func TestPackTimelineScore_TimezoneInvariant(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Skipf("skipping tz test: %v", err)
	}
	utc := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if PackTimelineScore(30, utc) != PackTimelineScore(30, utc.In(loc)) {
		t.Fatal("score must be timezone-invariant for the same instant")
	}
}

func TestUnpackTimelineRank_RoundTripsBucket(t *testing.T) {
	at := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	if got := UnpackTimelineRank(PackTimelineScore(30, at)); got != 30 {
		t.Fatalf("unpacked rank = %v, want 30", got)
	}
	if got := UnpackTimelineRank(-1); got != 0 {
		t.Fatalf("unpacked negative score = %v, want 0", got)
	}
}

func TestTimelineScoreFromTime_StableMonotonic(t *testing.T) {
	earlier := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Microsecond)

	if got := TimelineScoreFromTime(earlier); got != earlier.UnixMicro() {
		t.Fatalf("score(earlier) = %d, want %d", got, earlier.UnixMicro())
	}
	if TimelineScoreFromTime(later) <= TimelineScoreFromTime(earlier) {
		t.Fatal("score must be monotonic for monotonic time inputs")
	}
}

func TestTimelineScoreFromTime_NormalizesTimezone(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Skipf("skipping tz test: %v", err)
	}
	utc := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	local := utc.In(loc)

	if TimelineScoreFromTime(utc) != TimelineScoreFromTime(local) {
		t.Fatal("score must be timezone-invariant for the same instant")
	}
}

func TestTimelineScoreFromTime_TruncatesSubMicrosecond(t *testing.T) {
	base := time.Date(2025, 1, 1, 0, 0, 0, 1000, time.UTC) // 1µs in nanoseconds
	withSubMicro := base.Add(500 * time.Nanosecond)        // 1.5µs total
	if TimelineScoreFromTime(base) != TimelineScoreFromTime(withSubMicro) {
		t.Fatal("sub-microsecond drift must collapse to the same score (Redis ZSET tie-break by post_id)")
	}
}
