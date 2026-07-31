package entity

import (
	"strings"
	"testing"
)

// An empty template is the documented way to fall back to the bot's built-in
// prompt, so it has to validate.
func TestValidatePromptTemplate_EmptyIsTheDefault(t *testing.T) {
	if err := ValidatePromptTemplate(""); err != nil {
		t.Fatalf("empty template rejected: %v", err)
	}
}

func TestValidatePromptTemplate_AcceptsEveryExposedField(t *testing.T) {
	const tmpl = `{{.Username}} / {{.DisplayName}} / {{.Style}} / {{.Topic}} / {{.MaxTags}}
{{- range .Recent}}
- {{.}}
{{- end}}`

	if err := ValidatePromptTemplate(tmpl); err != nil {
		t.Fatalf("a template using the documented fields was rejected: %v", err)
	}
}

// Parsing alone accepts all of these; only executing the template catches them. An
// operator has to find out here rather than from a run of errors in the activity log.
func TestValidatePromptTemplate_RejectsWhatOnlyFailsAtRenderTime(t *testing.T) {
	for _, tt := range []struct {
		name string
		src  string
	}{
		{"unparseable", "{{.DisplayName"},
		{"field that does not exist", "viết như {{.NoSuchField}}"},
		{"method that does not exist", "{{.Topic.Nope}}"},
		{"index past the end of Recent", "{{index .Recent 9}}"},
		{"renders to nothing", "{{/* chỉ có chú thích */}}   "},
		{"renders to nothing conditionally", "{{if false}}xin chào{{end}}"},
		{"past the length limit", strings.Repeat("x", MaxPromptTemplateLen+1)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidatePromptTemplate(tt.src); err == nil {
				t.Fatal("expected a rejection")
			}
		})
	}
}

// The probe carries Recent entries on purpose: validating against an empty slice
// would let {{index .Recent 0}} through, and it would then fail on the bot host for
// every persona that had not posted yet.
func TestValidatePromptTemplate_ProbeExercisesRecent(t *testing.T) {
	if err := ValidatePromptTemplate("{{index .Recent 0}}"); err != nil {
		t.Fatalf("a template reading the first recent post should validate: %v", err)
	}
	if len(promptProbe.Recent) < 2 {
		t.Error("the probe must carry recent entries, or range and index bugs validate clean")
	}
}
