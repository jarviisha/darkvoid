// Package main (cmd/bot) is a standalone content bot that continuously
// publishes posts to a running DarkVoid API.
//
// The bot is a pure HTTP client with two upstreams:
//   - Gemini (AI Studio API) generates Vietnamese social-media post content
//     as structured JSON ({content, tags}).
//   - The DarkVoid API receives the posts: each bot persona registers itself
//     on first run (or logs in), then posts through POST /posts with a Bearer
//     token, so content flows through the real auth/hashtag/feed pipeline.
//
// Configuration comes from .env via pkg/config (BOT_* and GEMINI_* keys);
// flags override the interval, account count, and post limit. See main.go
// for usage.
package main
