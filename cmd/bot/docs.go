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
// # Where the bot's settings come from
//
// Not from here. The post interval, how many personas are active, the Gemini model
// fallback chain, the personas themselves, and the topic pool are all served by
// GET /bot/plan and edited through the admin API, so the bot re-reads them on every
// tick and an operator changes behaviour without redeploying or restarting anything.
// Each attempt is reported back to POST /bot/runs, which is the only reason its
// activity is visible anywhere other than this process's own logs.
//
// The environment therefore carries only credentials and an address: BOT_API_BASE_URL,
// BOT_PASSWORD (shared by the persona accounts), BOT_RUNNER_USERNAME/PASSWORD, and
// GEMINI_API_KEY. See config.go for the full list and main.go for usage.
//
// # Two kinds of account
//
// The runner account holds the bot role and is the only one allowed on /bot/*: it
// reads the plan and reports results. The personas hold no role at all and only
// publish their own posts. Keeping them separate means a persona's token cannot read
// or write the control plane, and the runner's cannot post as anyone.
package main
