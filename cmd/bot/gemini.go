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

	"github.com/jarviisha/darkvoid/pkg/logger"
)

const maxTagsPerPost = 3

// tagRegex mirrors the post-service tag rule so invalid tags are dropped
// client-side instead of failing the whole CreatePost call.
var tagRegex = regexp.MustCompile(`^[a-z0-9_]{1,50}$`)

// geminiClient calls the AI Studio generateContent endpoint.
//
// models is a priority-ordered fallback list: each GeneratePost tries them
// from the top and rotates to the next when one is rate-limited (429) or
// unavailable (404/503). Starting from the top every call means the preferred
// model is used again automatically once its per-day quota resets — no restart
// needed.
type geminiClient struct {
	baseURL string
	apiKey  string
	models  []string
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

	var lastErr error
	for i, model := range g.models {
		body, status, err := g.generateOnce(ctx, model, payload)
		if err != nil {
			// Transport-level failure (network, timeout): not model-specific,
			// but the next model may sit behind a healthier path, so rotate.
			lastErr = err
			continue
		}

		switch status {
		case http.StatusOK:
			if i > 0 {
				logger.Info(ctx, "gemini model rotated", "model", model)
			}
			return parseGeneratedPost(body)
		case http.StatusTooManyRequests, http.StatusNotFound, http.StatusServiceUnavailable:
			// Quota exhausted / model unavailable / overloaded: try the next.
			lastErr = fmt.Errorf("gemini model %q status %d: %s", model, status, truncate(string(body), 200))
			logger.Warn(ctx, "gemini model unavailable, rotating", "model", model, "status", status)
			continue
		default:
			// A real error (bad request, auth, 5xx) — rotating won't help.
			return nil, fmt.Errorf("gemini model %q status %d: %s", model, status, truncate(string(body), 300))
		}
	}

	return nil, fmt.Errorf("all gemini models exhausted: %w", lastErr)
}

// generateOnce performs a single generateContent call against one model and
// returns the raw body and HTTP status. Rotation logic lives in GeneratePost.
func (g *geminiClient) generateOnce(ctx context.Context, model string, payload []byte) ([]byte, int, error) {
	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", g.baseURL, model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, 0, fmt.Errorf("build gemini request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", g.apiKey)

	resp, err := g.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("call gemini: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read gemini response: %w", err)
	}
	return body, resp.StatusCode, nil
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
