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
// The same is true of what shapes the writing: the prompt template, the sampling
// temperature, how many tags to ask for, how many recent posts feed the repetition
// guard, and the two HTTP timeouts. Those were compile-time constants here until
// they moved to bot.config, which is why prompt.go keeps a default template rather
// than the prompt: the default is the floor under a failed plan fetch, not the
// normal path. See prompt.go for how a stored template is rendered and what happens
// when it turns out to be unusable.
//
// The environment therefore carries only credentials and an address: BOT_API_BASE_URL,
// BOT_PASSWORD (shared by the persona accounts), BOT_RUNNER_USERNAME/PASSWORD, and
// GEMINI_API_KEY. They live in .env.bot rather than the API server's .env — the bot
// shares none of that process's configuration and may not even run on the same host.
// .env is still read as a fallback, so an older setup keeps working; see config.go
// for the precedence and why it is ordered that way, and main.go for usage.
//
// # Two kinds of account
//
// The runner account holds the bot role and is the only one allowed on /bot/*: it
// reads the plan and reports results. The personas hold no role at all and only
// publish their own posts. Keeping them separate means a persona's token cannot read
// or write the control plane, and the runner's cannot post as anyone.
package main
