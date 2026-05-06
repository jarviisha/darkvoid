package feed

import (
	"testing"
	"time"
)

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
