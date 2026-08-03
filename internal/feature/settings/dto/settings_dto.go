package dto

// FeedSettingsResponse is the feed's current runtime configuration.
//
// Durations are carried as whole seconds rather than a Go duration string, matching
// bot.ConfigResponse and the column that stores them: the consumers of this API
// are an admin UI and curl, and "604800" needs no parser on either side.
type FeedSettingsResponse struct {
	TimelineEnabled        bool  `json:"timeline_enabled"          example:"false"`
	TimelineRolloutPercent int32 `json:"timeline_rollout_percent"  example:"0"`
	TimelineMaxItems       int32 `json:"timeline_max_items"        example:"1000"`
	TimelineTTLSeconds     int32 `json:"timeline_ttl_seconds"      example:"604800"`
	TimelineRefreshOnMiss  bool  `json:"timeline_refresh_on_miss"  example:"true"`

	FanoutEnabled      bool  `json:"fanout_enabled"       example:"true"`
	FanoutMaxFollowers int32 `json:"fanout_max_followers" example:"10000"`

	RelationshipBonus float64 `json:"relationship_bonus" example:"10"`
	RecencyScale      float64 `json:"recency_scale"      example:"20"`
	DecayExponent     float64 `json:"decay_exponent"     example:"1.5"`

	UpdatedBy *string `json:"updated_by,omitempty" example:"550e8400-e29b-41d4-a716-446655440000"`
	UpdatedAt string  `json:"updated_at"           example:"2026-07-27T10:30:00Z"`
}

// UpdateFeedSettingsRequest is a partial update — an omitted field keeps its
// stored value, and a request naming no field at all is rejected rather than
// recorded as an edit that changed nothing.
//
// Every field is a pointer, the booleans included. For a kill switch, false is
// the value an operator actually sends, so a plain bool would make "turn this
// off" indistinguishable from "leave it alone" — the one edit the endpoint exists
// for.
type UpdateFeedSettingsRequest struct {
	// TimelineEnabled gates prepared-timeline serving; TimelineRolloutPercent
	// (0-100) decides for what share of users. They are separate so a rollout can
	// be paused without losing the percentage to return to.
	TimelineEnabled        *bool  `json:"timeline_enabled,omitempty"         example:"true"`
	TimelineRolloutPercent *int32 `json:"timeline_rollout_percent,omitempty" example:"25"`
	// TimelineMaxItems is entries retained per user timeline, 1-10000. It is a
	// per-user memory cost in Redis, not a global one.
	TimelineMaxItems *int32 `json:"timeline_max_items,omitempty" example:"1000"`
	// TimelineTTLSeconds is how long a prepared timeline survives without a write,
	// 1 second to 90 days.
	TimelineTTLSeconds *int32 `json:"timeline_ttl_seconds,omitempty" example:"604800"`
	// TimelineRefreshOnMiss lets a miss rebuild that user's timeline inline, on the
	// request goroutine. Turn it off first when a cold cache is making reads slow.
	TimelineRefreshOnMiss *bool `json:"timeline_refresh_on_miss,omitempty" example:"true"`

	// FanoutEnabled is the kill switch for writing prepared timelines. The workers
	// stay running and idle, so it is reversible without a restart.
	FanoutEnabled *bool `json:"fanout_enabled,omitempty" example:"true"`
	// FanoutMaxFollowers bounds how many followers one event writes to.
	FanoutMaxFollowers *int32 `json:"fanout_max_followers,omitempty" example:"10000"`

	// The local ranking weights:
	//
	//	score = log(1+likes)*10 + recency_scale/(1+hours)^decay_exponent + relationship_bonus
	//
	// The three components are meant to sit on a comparable scale, which is what
	// the 0-1000 bounds protect: a relationship_bonus far above the others stops
	// ranking the feed and merely sorts followed authors to the top.
	RelationshipBonus *float64 `json:"relationship_bonus,omitempty" example:"10"`
	RecencyScale      *float64 `json:"recency_scale,omitempty"      example:"20"`
	// DecayExponent must be greater than 0. At 0 the recency term is the same
	// constant for every post, which removes recency from the formula rather than
	// flattening it — a small exponent is how to ask for a slow decay.
	DecayExponent *float64 `json:"decay_exponent,omitempty" example:"1.5"`
}
