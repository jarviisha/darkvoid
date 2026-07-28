// Package dto
package dto

import "github.com/jarviisha/darkvoid/internal/pagination"

// ─── Personas (admin plane) ──────────────────────────────────────────────────

// BotResponse is the admin view of one persona, including a summary of its recent
// activity. The error text behind LastErrorAt lives in the activity log rather
// than here, so listing personas stays at a fixed number of queries.
//
// LastSuccessAt and LastErrorAt are the most recent of each *within the activity
// log's retention window*, not for all time. Runs past it are deleted, so a persona
// whose last post predates the window reports no timestamp rather than a stale one.
type BotResponse struct {
	ID               string  `json:"id"                          example:"550e8400-e29b-41d4-a716-446655440000"`
	Username         string  `json:"username"                    example:"bot_sky"`
	DisplayName      string  `json:"display_name"                example:"Sky Vũ"`
	Style            string  `json:"style"                       example:"giọng trẻ trung, hài hước"`
	Enabled          bool    `json:"enabled"                     example:"true"`
	UserID           *string `json:"user_id,omitempty"           example:"550e8400-e29b-41d4-a716-446655440000"`
	RunRequestedAt   *string `json:"run_requested_at,omitempty"  example:"2026-07-27T10:30:00Z"`
	LastSuccessAt    *string `json:"last_success_at,omitempty"   example:"2026-07-27T10:28:00Z"`
	LastErrorAt      *string `json:"last_error_at,omitempty"     example:"2026-07-27T09:14:00Z"`
	SuccessesLast24h int64   `json:"successes_last_24h"          example:"41"`
	ErrorsLast24h    int64   `json:"errors_last_24h"             example:"2"`
	CreatedAt        string  `json:"created_at"                  example:"2026-07-27T08:00:00Z"`
	UpdatedAt        string  `json:"updated_at"                  example:"2026-07-27T10:30:00Z"`
}

// ListBotsResponse wraps every persona. There is no pagination: the pool is
// hand-curated and single-digit in practice.
type ListBotsResponse struct {
	Data []BotResponse `json:"data"`
}

// CreateBotRequest adds a persona. Username must satisfy the user-service rule
// ([a-zA-Z0-9_-]{3,30}) because the bot registers the account through the public
// auth API.
type CreateBotRequest struct {
	Username    string `json:"username"     example:"bot_sky"`
	DisplayName string `json:"display_name" example:"Sky Vũ"`
	Style       string `json:"style"        example:"giọng trẻ trung, hài hước, hay dùng emoji"`
}

// UpdateBotRequest is a partial update — an omitted field keeps its stored value.
// Username is absent on purpose: it is the registered account name, so renaming it
// would orphan the user the persona posts as.
type UpdateBotRequest struct {
	DisplayName *string `json:"display_name,omitempty" example:"Sky Vũ"`
	Style       *string `json:"style,omitempty"        example:"giọng trầm lắng, tản văn"`
	Enabled     *bool   `json:"enabled,omitempty"      example:"false"`
}

// ─── Config (admin plane) ────────────────────────────────────────────────────

// ConfigResponse is the current runtime configuration of the bot.
type ConfigResponse struct {
	PostIntervalSeconds int32    `json:"post_interval_seconds" example:"120"`
	Accounts            int32    `json:"accounts"              example:"3"`
	Models              []string `json:"models"                example:"gemini-2.5-flash"`
	Paused              bool     `json:"paused"                example:"false"`
	UpdatedBy           *string  `json:"updated_by,omitempty"  example:"550e8400-e29b-41d4-a716-446655440000"`
	UpdatedAt           string   `json:"updated_at"            example:"2026-07-27T10:30:00Z"`
}

// UpdateConfigRequest is a partial update — an omitted field keeps its stored
// value. An empty Models array counts as omitted, since an empty fallback chain
// would stall the bot and is rejected by the database anyway.
type UpdateConfigRequest struct {
	PostIntervalSeconds *int32   `json:"post_interval_seconds,omitempty" example:"120"`
	Accounts            *int32   `json:"accounts,omitempty"              example:"5"`
	Models              []string `json:"models,omitempty"                example:"gemini-2.5-flash"`
	Paused              *bool    `json:"paused,omitempty"                example:"true"`
}

// ─── Topics (admin plane) ────────────────────────────────────────────────────

// TopicResponse is one subject in the pool the bots draw from.
type TopicResponse struct {
	ID        string `json:"id"         example:"550e8400-e29b-41d4-a716-446655440000"`
	Content   string `json:"content"    example:"quán cà phê mới phát hiện ở góc phố quen"`
	Enabled   bool   `json:"enabled"    example:"true"`
	CreatedAt string `json:"created_at" example:"2026-07-27T08:00:00Z"`
}

// ListTopicsResponse wraps the whole topic pool.
type ListTopicsResponse struct {
	Data []TopicResponse `json:"data"`
}

// CreateTopicRequest adds a subject to the pool.
type CreateTopicRequest struct {
	Content string `json:"content" example:"một món ăn đường phố Việt Nam đáng nhớ"`
}

// SetTopicEnabledRequest retires or restores a subject without deleting it, so the
// activity log that referenced it stays meaningful.
type SetTopicEnabledRequest struct {
	Enabled bool `json:"enabled" example:"false"`
}

// ─── Activity log (admin plane) ──────────────────────────────────────────────

// RunResponse is one reported post attempt. PostPreview is filled in from the post
// service when it is wired, and omitted otherwise — the log stays readable either
// way rather than failing on a missing dependency.
type RunResponse struct {
	ID          string  `json:"id"                     example:"550e8400-e29b-41d4-a716-446655440000"`
	BotID       string  `json:"bot_id"                 example:"550e8400-e29b-41d4-a716-446655440000"`
	PostID      *string `json:"post_id,omitempty"      example:"550e8400-e29b-41d4-a716-446655440000"`
	PostPreview *string `json:"post_preview,omitempty" example:"Sáng nay cà phê ở góc phố quen…"`
	ModelUsed   *string `json:"model_used,omitempty"   example:"gemini-2.5-flash"`
	Status      string  `json:"status"                 example:"success"`
	Error       *string `json:"error,omitempty"        example:"gemini: all models exhausted (429)"`
	CreatedAt   string  `json:"created_at"             example:"2026-07-27T10:28:00Z"`
}

// ListRunsResponse wraps a paginated slice of the activity log, newest first.
type ListRunsResponse struct {
	Data []RunResponse `json:"data"`
	pagination.PaginationResponse
}

// ─── Agent plane ─────────────────────────────────────────────────────────────

// PlanResponse is the desired state the bot process polls for. It replaces what
// the bot used to read from its environment and compile-time slices, so a single
// fetch tells it everything it needs for the next tick.
type PlanResponse struct {
	// Paused tells the bot to keep polling but publish nothing.
	Paused              bool      `json:"paused"                example:"false"`
	PostIntervalSeconds int32     `json:"post_interval_seconds" example:"120"`
	Models              []string  `json:"models"                example:"gemini-2.5-flash"`
	Bots                []PlanBot `json:"bots"`
	Topics              []string  `json:"topics"                example:"quán cà phê mới phát hiện ở góc phố quen"`
}

// PlanBot is one active persona as the bot process sees it. Only enabled personas
// appear, already capped at the configured account count, so the bot posts as
// exactly these without applying any selection rules of its own.
type PlanBot struct {
	ID          string `json:"id"            example:"550e8400-e29b-41d4-a716-446655440000"`
	Username    string `json:"username"      example:"bot_sky"`
	DisplayName string `json:"display_name"  example:"Sky Vũ"`
	Style       string `json:"style"         example:"giọng trẻ trung, hài hước"`
	// RunRequested asks for an immediate post, out of band from the interval.
	RunRequested bool `json:"run_requested" example:"false"`
}

// ReportRunRequest is the bot reporting one attempt. A success must carry PostID
// and no Error; a failure must carry Error and no PostID.
type ReportRunRequest struct {
	BotID     string  `json:"bot_id"               example:"550e8400-e29b-41d4-a716-446655440000"`
	Status    string  `json:"status"               example:"success"`
	PostID    *string `json:"post_id,omitempty"    example:"550e8400-e29b-41d4-a716-446655440000"`
	ModelUsed *string `json:"model_used,omitempty" example:"gemini-2.5-flash"`
	Error     *string `json:"error,omitempty"      example:"gemini: all models exhausted (429)"`
	// HonoredRunRequest tells the server this attempt was the one an operator asked
	// for, which is what clears the pending flag. The bot must not set it for an
	// ordinary interval post: clearing unconditionally would discard a request that
	// arrived after the bot last fetched its plan, so the post would never happen.
	HonoredRunRequest bool `json:"honored_run_request" example:"false"`
}

// LinkIdentityRequest is the bot reporting which user account a persona posts as,
// once it has registered or logged it in.
type LinkIdentityRequest struct {
	UserID string `json:"user_id" example:"550e8400-e29b-41d4-a716-446655440000"`
}
