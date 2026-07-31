package main

import (
	"strings"
	"testing"
)

func testPromptData() promptData {
	p := testPersona()
	return p.promptData("chuyện đi làm", []string{"bài cũ số 1"}, defaultMaxTags)
}

// The built-in template is the floor under every other path in this file — a plan
// fetch that fails, a server too old to send one, an operator template that turns
// out to be broken. If it does not render, the bot has no prompt at all.
func TestBuildPrompt_BuiltInRendersPersonaTopicAndRecent(t *testing.T) {
	data := testPromptData()
	prompt := buildPrompt(t.Context(), "", data)

	for _, want := range []string{data.DisplayName, data.Style, "chuyện đi làm", "bài cũ số 1"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

// With the repetition guard off there is nothing to avoid repeating, so the section
// must not appear with an empty list under it.
func TestBuildPrompt_BuiltInOmitsRecentSectionWhenEmpty(t *testing.T) {
	data := testPromptData()
	data.Recent = nil

	if prompt := buildPrompt(t.Context(), "", data); strings.Contains(prompt, "Tránh lặp lại") {
		t.Errorf("prompt asks the model to avoid repeating nothing:\n%s", prompt)
	}
}

// The prompt's tag instruction is generated from MaxTags rather than fixed, so it
// cannot disagree with the cap sanitizeTags then enforces.
func TestBuildPrompt_BuiltInTagInstructionFollowsMaxTags(t *testing.T) {
	data := testPromptData()
	data.MaxTags = 5

	if prompt := buildPrompt(t.Context(), "", data); !strings.Contains(prompt, "1-5 tag") {
		t.Errorf("prompt does not ask for 1-5 tags:\n%s", prompt)
	}

	data.MaxTags = 0
	prompt := buildPrompt(t.Context(), "", data)
	if !strings.Contains(prompt, "Không kèm tag") {
		t.Errorf("prompt still asks for tags with the cap at 0:\n%s", prompt)
	}
}

func TestBuildPrompt_UsesTheStoredTemplate(t *testing.T) {
	got := buildPrompt(t.Context(), "viết như {{.DisplayName}} về {{.Topic}}", testPromptData())

	if want := "viết như Sky Vũ về chuyện đi làm"; got != want {
		t.Errorf("prompt = %q, want %q", got, want)
	}
}

// The admin API validates on write, so an unusable template here means the row was
// edited by hand or written by an older build. Posting in the default voice beats a
// persona that goes silent until somebody reads the activity log.
func TestBuildPrompt_FallsBackWhenTheStoredTemplateIsBroken(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  string
	}{
		{"unparseable", "{{.DisplayName"},
		{"unknown field", "viết như {{.NoSuchField}}"},
		{"index past the end", "{{index .Recent 9}}"},
		{"renders to nothing", "{{/* chỉ có chú thích */}}   "},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := buildPrompt(t.Context(), tt.src, testPromptData())

			if !strings.Contains(got, "chuyện đi làm") {
				t.Errorf("did not fall back to the built-in prompt:\n%s", got)
			}
		})
	}
}

// A whitespace-only template is stored as "no override", not as a template that
// renders to nothing — otherwise every post would log a fallback warning.
func TestBuildPrompt_TreatsBlankTemplateAsUnset(t *testing.T) {
	got := buildPrompt(t.Context(), "   \n\t ", testPromptData())

	if !strings.Contains(got, "chuyện đi làm") {
		t.Errorf("blank template did not fall back to the built-in prompt:\n%s", got)
	}
}

func TestRenderPrompt_RejectsAnEmptyRender(t *testing.T) {
	if _, err := renderPrompt("{{if false}}x{{end}}", testPromptData()); err == nil {
		t.Fatal("expected an error: an empty prompt makes Gemini return junk the bot would publish")
	}
}
