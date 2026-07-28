package entity

import (
	"math"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Bot is one content-bot persona: an account the bot process posts as, plus the
// writing voice it adopts. Personas used to be a compile-time slice in cmd/bot,
// so retiring or re-voicing one meant a rebuild.
type Bot struct {
	ID          uuid.UUID
	Username    string
	DisplayName string
	Style       string
	Enabled     bool
	// UserID is the usr.users row the bot posts as, nil until the bot process has
	// registered or logged in the account and reported it back.
	UserID *uuid.UUID
	// RunRequestedAt is set by an operator asking for an immediate post and cleared
	// by the bot once it has acted, so it doubles as a record of when it was asked.
	RunRequestedAt *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// RunRequested reports whether an immediate post is pending for this persona.
func (b *Bot) RunRequested() bool { return b.RunRequestedAt != nil }

// DurationToSeconds converts d to whole seconds, saturating at the int32 range
// that both the bot.config column and the API responses use. Every conversion goes
// through here so the narrowing is bounds-checked in one place instead of being
// assumed correct at each call site.
func DurationToSeconds(d time.Duration) int32 {
	seconds := int64(d / time.Second)
	if seconds > math.MaxInt32 {
		return math.MaxInt32
	}
	if seconds < 0 {
		return 0
	}
	return int32(seconds)
}

// Config holds the runtime knobs the bot process reads on every tick. These were
// BOT_POST_INTERVAL, BOT_ACCOUNTS, and GEMINI_MODELS in the environment; keeping
// them here is what lets an operator change them without a restart.
type Config struct {
	PostInterval time.Duration
	// Accounts caps how many enabled personas are active at once.
	Accounts int
	// Models is a priority-ordered Gemini fallback chain. The bot walks it top-down
	// and rotates on quota exhaustion, so an empty chain would stall it.
	Models []string
	// Paused is the global kill switch. It leaves each persona's Enabled flag alone,
	// so resuming restores exactly the previous selection.
	Paused    bool
	UpdatedBy *uuid.UUID
	UpdatedAt time.Time
}

// ConfigUpdate is a partial update: a nil field keeps its stored value. Models is
// a slice rather than a pointer because an empty chain is already invalid, so nil
// and empty can both mean "unchanged" without losing a meaningful state.
type ConfigUpdate struct {
	PostInterval *time.Duration
	Accounts     *int
	Models       []string
	Paused       *bool
	UpdatedBy    *uuid.UUID
}

// Topic is one subject the bots draw from at random.
type Topic struct {
	ID        uuid.UUID
	Content   string
	Enabled   bool
	CreatedAt time.Time
}

// RunStatus is the outcome of a single post attempt.
type RunStatus string

const (
	// RunSuccess means a post was published; the run carries its id.
	RunSuccess RunStatus = "success"
	// RunError means the attempt failed; the run carries the reason.
	RunError RunStatus = "error"
)

// runStatuses is the authoritative set, mirrored by the CHECK constraint on
// bot.runs.status.
var runStatuses = map[RunStatus]struct{}{
	RunSuccess: {},
	RunError:   {},
}

// ParseRunStatus converts a raw string into a RunStatus, reporting whether it
// names one the system recognises. Rejecting unknown values before they reach the
// database turns a CHECK violation into a validation error the caller can read.
func ParseRunStatus(s string) (RunStatus, bool) {
	status := RunStatus(s)
	_, ok := runStatuses[status]
	return status, ok
}

// String returns the status exactly as stored.
func (s RunStatus) String() string { return string(s) }

// Run is one reported post attempt — the activity log an operator reads. Before
// this existed the record lived only in the bot host's journal.
type Run struct {
	ID     uuid.UUID
	BotID  uuid.UUID
	PostID *uuid.UUID
	// ModelUsed is which entry of Config.Models actually answered, so quota
	// rotation is visible rather than inferred.
	ModelUsed *string
	Status    RunStatus
	Error     *string
	CreatedAt time.Time
}

// NewRun is a run being reported, before it has an id or timestamp.
type NewRun struct {
	BotID     uuid.UUID
	PostID    *uuid.UUID
	ModelUsed *string
	Status    RunStatus
	Error     *string
}

// OutcomeConsistent reports whether a run's fields agree with its status: a
// success carries a post id and no error, a failure carries a reason and no post
// id. This mirrors the bot_runs_outcome_consistent CHECK so an under-populated
// report is rejected as a validation error instead of a constraint violation —
// and because a success with no post id would otherwise read as a published post
// that produced nothing.
func (r NewRun) OutcomeConsistent() bool {
	switch r.Status {
	case RunSuccess:
		return r.PostID != nil && r.Error == nil
	case RunError:
		return r.PostID == nil && r.Error != nil
	default:
		return false
	}
}

// RunRetention is how long the activity log keeps a run. It bounds both the table
// and the cost of reading it: without a limit the log grows forever — at the default
// 30s interval that is ~2,880 rows per persona per day — and the per-persona summary
// on the admin list has to aggregate all of it on every page load.
//
// It is deliberately the only definition of the window. Both the prune and the stats
// query take it as a parameter, so a summary can never describe a period the log no
// longer covers.
const RunRetention = 30 * 24 * time.Hour

// RunStats summarises one persona's recent activity within RunRetention. The error
// text is deliberately absent: fetching it per persona would be a query per row,
// and the activity log already serves it.
type RunStats struct {
	BotID            uuid.UUID
	LastSuccessAt    *time.Time
	LastErrorAt      *time.Time
	SuccessesLast24h int64
	ErrorsLast24h    int64
}

// Bounds mirrored by the CHECK constraints on bot.config, enforced here too so a
// bad request fails with a readable message.
const (
	// MinPostInterval is the floor on Config.PostInterval.
	MinPostInterval = time.Second
	// MinAccounts is the floor on Config.Accounts.
	MinAccounts = 1
	// MaxAccounts is not a product limit — the effective cap is however many
	// personas exist. It keeps the value inside the SMALLINT column and rejects
	// obvious typos before they reach the database.
	MaxAccounts = 1000
)

// usernamePattern is the user-service username rule. A persona's username has to
// satisfy it because the bot registers the account through the public auth API,
// which would reject anything else.
var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,30}$`)

// ValidUsername reports whether s can be registered as a bot account.
func ValidUsername(s string) bool { return usernamePattern.MatchString(s) }

// MaxDisplayNameLen mirrors the VARCHAR(100) column on bot.bots, enforced in the
// service so an oversized name fails as a validation error rather than a string
// truncation error from the driver.
const MaxDisplayNameLen = 100

// ValidDisplayName reports whether s fits the display_name column. Postgres
// VARCHAR counts characters, not bytes, so this counts runes.
func ValidDisplayName(s string) bool { return utf8.RuneCountInString(s) <= MaxDisplayNameLen }
