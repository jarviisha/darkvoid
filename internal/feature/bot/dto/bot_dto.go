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
//
// PromptTemplate is returned exactly as stored, empty string included. Empty means
// the bot falls back to its built-in prompt, so an admin UI should render it as a
// placeholder rather than as "no prompt".
type ConfigResponse struct {
	PostIntervalSeconds int32    `json:"post_interval_seconds" example:"120"`
	Accounts            int32    `json:"accounts"              example:"3"`
	Models              []string `json:"models"                example:"gemini-2.5-flash"`
	Paused              bool     `json:"paused"                example:"false"`

	PromptTemplate       string  `json:"prompt_template"        example:""`
	Temperature          float64 `json:"temperature"            example:"1"`
	MaxTagsPerPost       int32   `json:"max_tags_per_post"      example:"3"`
	RecentMemory         int32   `json:"recent_memory"          example:"5"`
	APITimeoutSeconds    int32   `json:"api_timeout_seconds"    example:"15"`
	GeminiTimeoutSeconds int32   `json:"gemini_timeout_seconds" example:"60"`

	UpdatedBy *string `json:"updated_by,omitempty"  example:"550e8400-e29b-41d4-a716-446655440000"`
	UpdatedAt string  `json:"updated_at"            example:"2026-07-27T10:30:00Z"`
}

// UpdateConfigRequest is a partial update — an omitted field keeps its stored
// value. An empty Models array counts as omitted, since an empty fallback chain
// would stall the bot and is rejected by the database anyway.
//
// PromptTemplate is a pointer rather than a plain string for the opposite reason:
// an empty template is the documented way back to the bot's built-in prompt, so
// "omitted" and "set to empty" have to stay distinguishable.
type UpdateConfigRequest struct {
	PostIntervalSeconds *int32   `json:"post_interval_seconds,omitempty" example:"120"`
	Accounts            *int32   `json:"accounts,omitempty"              example:"5"`
	Models              []string `json:"models,omitempty"                example:"gemini-2.5-flash"`
	Paused              *bool    `json:"paused,omitempty"                example:"true"`

	// PromptTemplate is a Go text/template rendered against the persona, the drawn
	// topic and the recent-post list: {{.DisplayName}}, {{.Username}}, {{.Style}},
	// {{.Topic}}, {{.MaxTags}}, and {{range .Recent}}. It is validated by rendering
	// it, so a reference to a field that does not exist is a 400 here rather than a
	// run error on the bot host.
	PromptTemplate *string `json:"prompt_template,omitempty" example:"Bạn là {{.DisplayName}}..."`
	// Temperature is Gemini's sampling temperature, 0 to 2.
	Temperature *float64 `json:"temperature,omitempty" example:"1"`
	// MaxTagsPerPost is capped at the post service's own tag limit of 10.
	MaxTagsPerPost *int32 `json:"max_tags_per_post,omitempty" example:"3"`
	// RecentMemory is how many recent posts feed the repetition guard; 0 disables it.
	RecentMemory *int32 `json:"recent_memory,omitempty" example:"5"`
	// APITimeoutSeconds and GeminiTimeoutSeconds bound one HTTP request each in the
	// bot process. They take effect on the tick after the bot next fetches its plan.
	APITimeoutSeconds    *int32 `json:"api_timeout_seconds,omitempty"    example:"15"`
	GeminiTimeoutSeconds *int32 `json:"gemini_timeout_seconds,omitempty" example:"60"`
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

	// The generation knobs, which were compile-time constants in cmd/bot. They ride
	// on the plan rather than on a separate endpoint so one fetch still tells the bot
	// everything it needs for the tick — a second call could fail on its own and
	// leave the bot generating under half a configuration.
	//
	// PromptTemplate is empty when no template is stored, which the bot reads as
	// "use your built-in default". The server does not substitute the default here:
	// it does not have one, deliberately, so that there is exactly one copy.
	PromptTemplate       string  `json:"prompt_template"        example:""`
	Temperature          float64 `json:"temperature"            example:"1"`
	MaxTagsPerPost       int32   `json:"max_tags_per_post"      example:"3"`
	RecentMemory         int32   `json:"recent_memory"          example:"5"`
	APITimeoutSeconds    int32   `json:"api_timeout_seconds"    example:"15"`
	GeminiTimeoutSeconds int32   `json:"gemini_timeout_seconds" example:"60"`
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
