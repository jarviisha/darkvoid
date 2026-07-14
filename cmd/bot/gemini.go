package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

const maxTagsPerPost = 3

// tagRegex mirrors the post-service tag rule so invalid tags are dropped
// client-side instead of failing the whole CreatePost call.
var tagRegex = regexp.MustCompile(`^[a-z0-9_]{1,50}$`)

// geminiClient calls the AI Studio generateContent endpoint.
type geminiClient struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// generatedPost is the structured output Gemini is asked to return.
type generatedPost struct {
	Content string   `json:"content"`
	Tags    []string `json:"tags"`
}

// generateContentRequest is the subset of the Gemini REST API the bot uses.
type generateContentRequest struct {
	Contents         []geminiContent  `json:"contents"`
	GenerationConfig generationConfig `json:"generationConfig"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type generationConfig struct {
	Temperature      float64         `json:"temperature"`
	ResponseMimeType string          `json:"responseMimeType"`
	ResponseSchema   json.RawMessage `json:"responseSchema"`
}

// postSchema forces Gemini into JSON mode with the generatedPost shape.
var postSchema = json.RawMessage(`{
	"type": "OBJECT",
	"properties": {
		"content": {"type": "STRING"},
		"tags": {"type": "ARRAY", "items": {"type": "STRING"}}
	},
	"required": ["content"]
}`)

type generateContentResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

// GeneratePost asks Gemini for one post in the persona's voice.
func (g *geminiClient) GeneratePost(ctx context.Context, p persona, topic string, recent []string) (*generatedPost, error) {
	reqBody := generateContentRequest{
		Contents: []geminiContent{{
			Role:  "user",
			Parts: []geminiPart{{Text: buildPrompt(p, topic, recent)}},
		}},
		GenerationConfig: generationConfig{
			Temperature:      1.0,
			ResponseMimeType: "application/json",
			ResponseSchema:   postSchema,
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal gemini request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", g.baseURL, g.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build gemini request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.apiKey)

	resp, err := g.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call gemini: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read gemini response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini status %d: %s", resp.StatusCode, truncate(string(body), 300))
	}

	return parseGeneratedPost(body)
}

// parseGeneratedPost extracts the JSON post from a generateContent response
// and sanitizes it against DarkVoid's post rules.
func parseGeneratedPost(body []byte) (*generatedPost, error) {
	var resp generateContentResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("decode gemini response: %w", err)
	}
	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return nil, fmt.Errorf("gemini returned no candidates")
	}

	var post generatedPost
	text := resp.Candidates[0].Content.Parts[0].Text
	if err := json.Unmarshal([]byte(text), &post); err != nil {
		return nil, fmt.Errorf("decode generated post %q: %w", truncate(text, 120), err)
	}

	post.Content = strings.TrimSpace(post.Content)
	if post.Content == "" {
		return nil, fmt.Errorf("gemini returned empty content")
	}
	post.Tags = sanitizeTags(post.Tags)
	return &post, nil
}

// sanitizeTags lowercases tags, strips leading '#', drops anything that
// still violates the post-service tag rule, and caps the count.
func sanitizeTags(tags []string) []string {
	out := make([]string, 0, min(len(tags), maxTagsPerPost))
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(t, "#")))
		if !tagRegex.MatchString(t) {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
		if len(out) == maxTagsPerPost {
			break
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
