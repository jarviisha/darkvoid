# Codohue that cannot be built fails the boot; Codohue that cannot be reached does not

`CODOHUE_ENABLED=true` with no `CODOHUE_BASE_URL` refuses the boot. An enabled Codohue that is merely unreachable does not: the client is wired, `/health` reports `degraded`, and a background monitor watches for recovery.

The asymmetry is deliberate and reads as a contradiction without it. An outage is transient and already handled per call — the feed falls back to local scoring and ingest logs and moves on — so failing the boot for one would disable the integration for the life of the process over a problem that fixes itself. A missing base URL is neither transient nor recoverable: it fails identically on every restart, and no monitor can resolve it.

It also used to be silent in the worst way. `codohue.NewClient` answered a construction failure with a nil `*Client` and no error, which the feed stored in its `Recommender` field as a non-nil interface over a nil pointer — so `/health` reported `codohue: off` while the first feed request dereferenced the breaker.

## Extended, 2026-09-24

The same reasoning now covers two credentials, not just the URL. `validateCodohue`
refuses to boot when `CODOHUE_ENABLED=true` and `CODOHUE_NAMESPACE_KEY` is empty,
or when `CODOHUE_ADMIN_URL` is set without `CODOHUE_ADMIN_TOKEN`.

Neither is a condition a monitor can clear. The namespace key stopped being
server-minted — provisioning sends it now — so an empty one is not "waiting to be
issued", it is a deployment that will 401 on every call for as long as it runs.
The admin token is what Codohue v0.12.0 left in place of the global admin key, and
without it provisioning gets a 403 it cannot retry out of.

Both therefore sit on this ADR's "cannot be built" side rather than its "answer
degraded and keep serving" side, which stays reserved for a Codohue that is merely
unreachable.
