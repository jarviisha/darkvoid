// Package handler serves both of the bot control plane's HTTP surfaces.
//
// The /admin/bots routes are an operator's; they are registered inside the
// existing /admin group so they inherit its admin role check rather than
// declaring one of their own.
//
// The /bot routes are the bot process's. They are guarded by the bot role, held by
// a single runner account — not by the persona accounts. The runner polls the plan
// and reports runs; each persona logs in separately only to publish its own posts.
// That is why a run report names its persona in the body: the token identifies the
// runner, which is trusted to report on behalf of any persona.
package handler
