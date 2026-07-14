package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// tokenSafetyMargin re-logins slightly before the access token actually
// expires so a post never races token expiry.
const tokenSafetyMargin = 30 * time.Second

// apiClient is a minimal DarkVoid API client for the bot: register/login
// bot accounts and create posts on their behalf.
type apiClient struct {
	baseURL string
	http    *http.Client
	now     func() time.Time
}

// botAccount is one authenticated persona.
type botAccount struct {
	persona     persona
	password    string
	accessToken string
	expiresAt   time.Time
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type registerRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

// tokenResponse covers the fields shared by login and register responses.
type tokenResponse struct {
	AccessToken     string `json:"access_token"`
	AccessExpiresIn int64  `json:"access_expires_in"`
}

type createPostRequest struct {
	Content    string   `json:"content"`
	Visibility string   `json:"visibility"`
	Tags       []string `json:"tags,omitempty"`
}

type createPostResponse struct {
	ID string `json:"id"`
}

// EnsureLogin makes sure the account holds a valid access token,
// registering the account on first use.
func (c *apiClient) EnsureLogin(ctx context.Context, acc *botAccount) error {
	if acc.accessToken != "" && c.now().Before(acc.expiresAt) {
		return nil
	}

	var tok tokenResponse
	status, body, err := c.postJSON(ctx, "/auth/login", "", loginRequest{
		Username: acc.persona.username,
		Password: acc.password,
	}, &tok)
	if err != nil {
		return err
	}

	if status == http.StatusUnauthorized || status == http.StatusNotFound {
		// Account doesn't exist yet — register it (register returns tokens).
		status, body, err = c.postJSON(ctx, "/auth/register", "", registerRequest{
			Username:    acc.persona.username,
			Email:       acc.persona.username + "@bot.local",
			DisplayName: acc.persona.displayName,
			Password:    acc.password,
		}, &tok)
		if err != nil {
			return err
		}
		if status != http.StatusCreated && status != http.StatusOK {
			return fmt.Errorf("register %s: status %d: %s", acc.persona.username, status, truncate(string(body), 200))
		}
	} else if status != http.StatusOK {
		return fmt.Errorf("login %s: status %d: %s", acc.persona.username, status, truncate(string(body), 200))
	}

	if tok.AccessToken == "" {
		return fmt.Errorf("auth for %s returned no access token", acc.persona.username)
	}
	acc.accessToken = tok.AccessToken
	acc.expiresAt = c.now().Add(time.Duration(tok.AccessExpiresIn)*time.Second - tokenSafetyMargin)
	return nil
}

// CreatePost publishes a public post as the given account, transparently
// re-authenticating once if the token was rejected.
func (c *apiClient) CreatePost(ctx context.Context, acc *botAccount, content string, tags []string) (string, error) {
	if err := c.EnsureLogin(ctx, acc); err != nil {
		return "", err
	}

	body := createPostRequest{Content: content, Visibility: "public", Tags: tags}

	var created createPostResponse
	status, respBody, err := c.postJSON(ctx, "/posts", acc.accessToken, body, &created)
	if err != nil {
		return "", err
	}

	if status == http.StatusUnauthorized {
		// Token revoked server-side (e.g. restart with new secret) — retry once.
		acc.accessToken = ""
		if err = c.EnsureLogin(ctx, acc); err != nil {
			return "", err
		}
		status, respBody, err = c.postJSON(ctx, "/posts", acc.accessToken, body, &created)
		if err != nil {
			return "", err
		}
	}

	if status != http.StatusCreated {
		return "", fmt.Errorf("create post as %s: status %d: %s", acc.persona.username, status, truncate(string(respBody), 200))
	}
	return created.ID, nil
}

// postJSON sends a JSON POST and decodes a 2xx body into out (when non-nil).
// Non-2xx statuses are returned to the caller for flow decisions (the raw
// body comes along for error messages); only transport-level failures
// produce an error.
func (c *apiClient) postJSON(ctx context.Context, path, token string, in, out any) (int, []byte, error) {
	payload, err := json.Marshal(in)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal %s request: %w", path, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, fmt.Errorf("build %s request: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("call %s: %w", path, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read %s response: %w", path, err)
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 && out != nil {
		if err = json.Unmarshal(body, out); err != nil {
			return resp.StatusCode, body, fmt.Errorf("decode %s response: %w", path, err)
		}
	}
	return resp.StatusCode, body, nil
}
