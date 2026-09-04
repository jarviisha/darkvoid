# Codohue that cannot be built fails the boot; Codohue that cannot be reached does not

`CODOHUE_ENABLED=true` with no `CODOHUE_BASE_URL` refuses the boot. An enabled Codohue that is merely unreachable does not: the client is wired, `/health` reports `degraded`, and a background monitor watches for recovery.

The asymmetry is deliberate and reads as a contradiction without it. An outage is transient and already handled per call — the feed falls back to local scoring and ingest logs and moves on — so failing the boot for one would disable the integration for the life of the process over a problem that fixes itself. A missing base URL is neither transient nor recoverable: it fails identically on every restart, and no monitor can resolve it.

It also used to be silent in the worst way. `codohue.NewClient` answered a construction failure with a nil `*Client` and no error, which the feed stored in its `Recommender` field as a non-nil interface over a nil pointer — so `/health` reported `codohue: off` while the first feed request dereferenced the breaker.
