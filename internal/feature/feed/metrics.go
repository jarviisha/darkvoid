package feed

import (
	"strings"
	"sync/atomic"
	"time"
)

var metrics feedMetrics

type feedMetrics struct {
	cursorRejections      atomic.Uint64
	dispatchEnqueueFailed atomic.Uint64
	dispatchQueueDepth    atomic.Int64
	timelineHits          atomic.Uint64
	timelineMisses        atomic.Uint64
	timelineReadErrors    atomic.Uint64
	lazyRefreshes         atomic.Uint64
	fallbacks             atomic.Uint64
	staleFiltered         atomic.Uint64
	fanoutProcessed       atomic.Uint64
	fanoutErrors          atomic.Uint64
	fanoutCapped          atomic.Uint64
	fanoutLatencyMsTotal  atomic.Uint64
	redisMemoryPressure   atomic.Uint64
}

// MetricsSnapshot is a point-in-time copy of feed operational counters.
type MetricsSnapshot struct {
	CursorRejections      uint64 `json:"cursor_rejections"`
	DispatchEnqueueFailed uint64 `json:"dispatch_enqueue_failed"`
	DispatchQueueDepth    int64  `json:"dispatch_queue_depth"`
	TimelineHits          uint64 `json:"timeline_hits"`
	TimelineMisses        uint64 `json:"timeline_misses"`
	TimelineReadErrors    uint64 `json:"timeline_read_errors"`
	LazyRefreshes         uint64 `json:"lazy_refreshes"`
	Fallbacks             uint64 `json:"fallbacks"`
	StaleFiltered         uint64 `json:"stale_filtered"`
	FanoutProcessed       uint64 `json:"fanout_processed"`
	FanoutErrors          uint64 `json:"fanout_errors"`
	FanoutCapped          uint64 `json:"fanout_capped"`
	FanoutLatencyMsTotal  uint64 `json:"fanout_latency_ms_total"`
	RedisMemoryPressure   uint64 `json:"redis_memory_pressure"`
}

// SnapshotMetrics returns current feed operational counters.
func SnapshotMetrics() MetricsSnapshot {
	return MetricsSnapshot{
		CursorRejections:      metrics.cursorRejections.Load(),
		DispatchEnqueueFailed: metrics.dispatchEnqueueFailed.Load(),
		DispatchQueueDepth:    metrics.dispatchQueueDepth.Load(),
		TimelineHits:          metrics.timelineHits.Load(),
		TimelineMisses:        metrics.timelineMisses.Load(),
		TimelineReadErrors:    metrics.timelineReadErrors.Load(),
		LazyRefreshes:         metrics.lazyRefreshes.Load(),
		Fallbacks:             metrics.fallbacks.Load(),
		StaleFiltered:         metrics.staleFiltered.Load(),
		FanoutProcessed:       metrics.fanoutProcessed.Load(),
		FanoutErrors:          metrics.fanoutErrors.Load(),
		FanoutCapped:          metrics.fanoutCapped.Load(),
		FanoutLatencyMsTotal:  metrics.fanoutLatencyMsTotal.Load(),
		RedisMemoryPressure:   metrics.redisMemoryPressure.Load(),
	}
}

func CountCursorRejected() { metrics.cursorRejections.Add(1) }

func CountDispatchEnqueueFailed() { metrics.dispatchEnqueueFailed.Add(1) }

func SetDispatchQueueDepth(depth int) { metrics.dispatchQueueDepth.Store(int64(depth)) }

func CountTimelineHit() { metrics.timelineHits.Add(1) }

func CountTimelineMiss() { metrics.timelineMisses.Add(1) }

func CountTimelineReadError() { metrics.timelineReadErrors.Add(1) }

func CountLazyRefresh() { metrics.lazyRefreshes.Add(1) }

func CountFallback() { metrics.fallbacks.Add(1) }

func CountStaleFiltered(count int) {
	if count > 0 {
		metrics.staleFiltered.Add(uint64(count))
	}
}

func ObserveFanoutProcessed(duration time.Duration) {
	metrics.fanoutProcessed.Add(1)
	latencyMs := duration.Milliseconds()
	if latencyMs > 0 {
		metrics.fanoutLatencyMsTotal.Add(uint64(latencyMs))
	}
}

func CountFanoutError() { metrics.fanoutErrors.Add(1) }

func CountFanoutCapped() { metrics.fanoutCapped.Add(1) }

// ObserveRedisError records Redis memory-pressure signals when Redis exposes them.
func ObserveRedisError(err error) {
	if err == nil {
		return
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "oom") || strings.Contains(msg, "maxmemory") || strings.Contains(msg, "memory") {
		metrics.redisMemoryPressure.Add(1)
	}
}
