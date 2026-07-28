package entity

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestParseRunStatus_KnownStatuses(t *testing.T) {
	for _, want := range []RunStatus{RunSuccess, RunError} {
		got, ok := ParseRunStatus(want.String())
		if !ok {
			t.Errorf("ParseRunStatus(%q) should be recognised", want)
		}
		if got != want {
			t.Errorf("ParseRunStatus(%q) = %q", want, got)
		}
	}
}

func TestParseRunStatus_UnknownRejected(t *testing.T) {
	for _, name := range []string{"", "Success", "ok", "failed", "success "} {
		if _, ok := ParseRunStatus(name); ok {
			t.Errorf("ParseRunStatus(%q) should be rejected — it would fail the CHECK constraint", name)
		}
	}
}

func TestOutcomeConsistent(t *testing.T) {
	postID := uuid.New()
	reason := "quota exhausted"

	tests := []struct {
		name string
		run  NewRun
		want bool
	}{
		{
			name: "success with post id",
			run:  NewRun{Status: RunSuccess, PostID: &postID},
			want: true,
		},
		{
			name: "error with reason",
			run:  NewRun{Status: RunError, Error: &reason},
			want: true,
		},
		{
			// The case the CHECK exists for: this would read as a published post that
			// produced nothing.
			name: "success without post id",
			run:  NewRun{Status: RunSuccess},
			want: false,
		},
		{
			name: "success carrying an error",
			run:  NewRun{Status: RunSuccess, PostID: &postID, Error: &reason},
			want: false,
		},
		{
			name: "error without a reason",
			run:  NewRun{Status: RunError},
			want: false,
		},
		{
			name: "error carrying a post id",
			run:  NewRun{Status: RunError, PostID: &postID, Error: &reason},
			want: false,
		},
		{
			name: "unparsed status",
			run:  NewRun{Status: RunStatus("pending"), PostID: &postID},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.run.OutcomeConsistent(); got != tt.want {
				t.Errorf("OutcomeConsistent() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The pattern has to agree with the user service, since the bot registers its
// accounts through the public auth API.
func TestValidUsername(t *testing.T) {
	valid := []string{"bot_sky", "bot-dulich", "abc", "Bot123", "a1234567890123456789012345678z"}
	for _, s := range valid {
		if !ValidUsername(s) {
			t.Errorf("ValidUsername(%q) = false, want true", s)
		}
	}

	invalid := []string{"", "ab", "bot sky", "bot.sky", "bot@local", "bột_sky",
		"a12345678901234567890123456789zz"}
	for _, s := range invalid {
		if ValidUsername(s) {
			t.Errorf("ValidUsername(%q) = true, want false", s)
		}
	}
}

// The config column and the API both carry seconds as an int32, so the narrowing
// has to saturate rather than wrap — a wrapped interval could come back negative
// and stall the bot.
func TestDurationToSeconds(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want int32
	}{
		{"whole seconds", 30 * time.Second, 30},
		{"minutes", 2 * time.Minute, 120},
		{"truncates a partial second", 1500 * time.Millisecond, 1},
		{"zero", 0, 0},
		{"negative saturates at zero", -5 * time.Second, 0},
		{"overflow saturates at the int32 max", time.Duration(math.MaxInt64), math.MaxInt32},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DurationToSeconds(tt.in); got != tt.want {
				t.Errorf("DurationToSeconds(%v) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestBot_RunRequested(t *testing.T) {
	var b Bot
	if b.RunRequested() {
		t.Error("a persona with no request pending should report false")
	}
	now := b.CreatedAt
	b.RunRequestedAt = &now
	if !b.RunRequested() {
		t.Error("a persona with RunRequestedAt set should report true")
	}
}
