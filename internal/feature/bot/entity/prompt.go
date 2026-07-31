package entity

import (
	"fmt"
	"strings"
	"text/template"
	"unicode/utf8"
)

// PromptData is the value a stored prompt template is rendered against. It is the
// contract between an operator writing a template through /admin/bots and cmd/bot
// rendering it on the next tick.
//
// The bot declares its own identical struct rather than importing this one — it is
// an ordinary HTTP client and stays one, the same reason it does not import the
// DTOs. The two are pinned together by a contract test in cmd/bot, so a field
// renamed on one side fails `make test` instead of failing at render time on
// whichever host the bot happens to be running.
type PromptData struct {
	// Username is the persona's account name, mostly useful as a stable handle when
	// a template wants to address the persona rather than describe it.
	Username string
	// DisplayName and Style are the persona's identity and writing voice.
	DisplayName string
	Style       string
	// Topic is the subject drawn from the enabled topic pool for this post.
	Topic string
	// Recent holds openings of the persona's latest posts, oldest first, as the
	// repetition guard. Empty when RecentMemory is 0 or the bot has just started, so
	// a template must not assume it has entries — range over it rather than indexing.
	Recent []string
	// MaxTags is Config.MaxTagsPerPost, exposed so the prompt's tag instruction and
	// the client-side cap cannot drift apart.
	MaxTags int
}

// MaxPromptTemplateLen bounds a stored template. It is a sanity limit rather than a
// model limit: the template is sent to Gemini on every single post, so a runaway
// paste is a cost multiplied by the post rate.
const MaxPromptTemplateLen = 8000

// promptProbe is the sample rendered during validation. Recent deliberately carries
// entries — a template that indexes into it would validate against an empty slice
// and then panic on the first real render.
var promptProbe = PromptData{
	Username:    "bot_probe",
	DisplayName: "Probe",
	Style:       "giọng trung tính",
	Topic:       "một chủ đề mẫu",
	Recent:      []string{"bài gần đây thứ nhất", "bài gần đây thứ hai"},
	MaxTags:     3,
}

// ValidatePromptTemplate reports whether s can be rendered by the bot. An empty
// string is valid and means "use the bot's built-in default": the default has to
// exist in cmd/bot regardless, because a failed plan fetch must not leave the bot
// with no prompt at all, and storing a second copy here would let the two drift.
//
// Validation executes the template rather than only parsing it. Parsing accepts
// {{.Nonexistent}} and {{index .Recent 5}} quite happily; both fail at execution,
// and by then the failure is a run-error on a remote host rather than a 400 the
// operator sees while editing.
func ValidatePromptTemplate(s string) error {
	if s == "" {
		return nil
	}
	if utf8.RuneCountInString(s) > MaxPromptTemplateLen {
		return fmt.Errorf("must be at most %d characters", MaxPromptTemplateLen)
	}

	tmpl, err := template.New("prompt").Option("missingkey=error").Parse(s)
	if err != nil {
		return fmt.Errorf("is not a valid Go template: %w", err)
	}

	rendered, err := RenderPrompt(tmpl, promptProbe)
	if err != nil {
		return fmt.Errorf("fails to render: %w", err)
	}
	// A template of nothing but whitespace or comments parses and executes fine and
	// then asks Gemini for a post about nothing, which returns junk the bot dutifully
	// publishes. Rejecting it here is the only place that catches it.
	if strings.TrimSpace(rendered) == "" {
		return fmt.Errorf("renders to an empty prompt")
	}
	return nil
}

// RenderPrompt executes tmpl against data. It exists so validation renders through
// exactly the same path the bot does, including the panic guard: a template can
// panic during execution (indexing past a slice does), and text/template recovers
// those into an error only for its own internal panics — a nil map index, for one,
// propagates. An admin request must not take the API process down.
func RenderPrompt(tmpl *template.Template, data PromptData) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panicked during execution: %v", r)
		}
	}()

	var b strings.Builder
	if execErr := tmpl.Execute(&b, data); execErr != nil {
		return "", execErr
	}
	return b.String(), nil
}
