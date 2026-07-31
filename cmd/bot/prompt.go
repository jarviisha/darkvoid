package main

import (
	"context"
	"fmt"
	"strings"
	"text/template"

	"github.com/jarviisha/darkvoid/pkg/logger"
)

// promptData is what a prompt template is rendered against. It mirrors
// entity.PromptData on the server, which is what /admin/bots validates a submitted
// template against.
//
// The two structs are declared separately rather than shared: the bot is an
// ordinary HTTP client and does not import the server's packages, the same reason
// it declares its own copies of the DTOs. contract_test.go pins the field sets
// together, so renaming one side fails `make test` instead of turning every
// template an operator has written into a render error on the bot host.
type promptData struct {
	Username    string
	DisplayName string
	Style       string
	Topic       string
	Recent      []string
	MaxTags     int
}

// defaultPromptTemplate is the prompt the bot uses when bot.config stores none.
// This is the only copy: the server deliberately does not hold a default of its
// own, because two copies would drift and the one that mattered would be whichever
// happened to be deployed.
//
// It is a template rather than the old fmt.Fprintf so that an operator-supplied
// template and the built-in one go through exactly one rendering path — a fallback
// that behaves differently from the thing it falls back to is worse than no
// fallback at all.
const defaultPromptTemplate = `Bạn là "{{.DisplayName}}", một người dùng mạng xã hội Việt Nam. Phong cách viết: {{.Style}}.

Hãy viết đúng MỘT bài đăng mạng xã hội bằng tiếng Việt về chủ đề: {{.Topic}}.

Yêu cầu:
- Dài 1-4 câu, tự nhiên như người thật viết, không mở đầu bằng lời chào.
- Không dùng hashtag trong phần nội dung.
{{- if gt .MaxTags 0}}
- Kèm 1-{{.MaxTags}} tag ngắn (chỉ chữ thường không dấu, số, gạch dưới; ví dụ "caphe", "hanoi", "worklife").
{{- else}}
- Không kèm tag nào.
{{- end}}
{{- if .Recent}}

Tránh lặp lại ý của các bài gần đây:
{{- range .Recent}}
- {{.}}
{{- end}}
{{- end}}

Trả về JSON: {"content": "...", "tags": ["..."]}`

// buildPrompt renders the prompt for one post. src is the template from the plan;
// an empty one means the operator has not overridden the default.
//
// A template that fails to parse or render falls back to the default and logs,
// rather than failing the post. The admin API validates on write, so getting here
// means the row was edited by hand or written by an older build — and in that case
// posting in the default voice is a better outcome than a persona that goes silent
// until someone reads the activity log.
func buildPrompt(ctx context.Context, src string, data promptData) string {
	if strings.TrimSpace(src) != "" {
		rendered, err := renderPrompt(src, data)
		if err == nil {
			return rendered
		}
		logger.Warn(ctx, "stored prompt template is unusable, falling back to the built-in one",
			"error", err, "bot", data.Username)
	}

	rendered, err := renderPrompt(defaultPromptTemplate, data)
	if err != nil {
		// Unreachable unless the constant above is edited into something invalid,
		// which prompt_test.go rejects at build time. Returning the raw template is
		// still better than an empty prompt: it names the persona and the topic.
		logger.Error(ctx, "the built-in prompt template failed to render", "error", err)
		return defaultPromptTemplate
	}
	return rendered
}

// renderPrompt parses and executes src, recovering from a panic during execution.
// text/template turns most execution faults into errors, but not all of them, and a
// template written by an operator is untrusted input as far as this process is
// concerned — a panic here would take down a long-running bot over a typo.
func renderPrompt(src string, data promptData) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("prompt template panicked during execution: %v", r)
		}
	}()

	tmpl, err := template.New("prompt").Option("missingkey=error").Parse(src)
	if err != nil {
		return "", fmt.Errorf("parse prompt template: %w", err)
	}

	var b strings.Builder
	if execErr := tmpl.Execute(&b, data); execErr != nil {
		return "", fmt.Errorf("render prompt template: %w", execErr)
	}
	if strings.TrimSpace(b.String()) == "" {
		// Asking Gemini for a post about nothing returns junk that the bot would then
		// publish, so an empty render is treated as a broken template.
		return "", fmt.Errorf("prompt template rendered to nothing")
	}
	return b.String(), nil
}
