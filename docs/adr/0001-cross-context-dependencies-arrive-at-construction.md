# Cross-context dependencies arrive at construction

Services used to receive their cross-context collaborators through `With…` setters called from the composition root after construction. Every target field was an interface whose zero value is nil, every consumer guarded with "if nil, skip", and nothing asserted the setter had run — so a forgotten wire changed behaviour instead of failing. A missing follow checker, for instance, made every followers-only post answer 404 to exactly the followers entitled to read it. Services now take a `Deps` struct and return an error naming every missing field.

## Considered options

Hoisting repository construction out of each `Setup*Context` into one step ahead of every context would have removed the last deferred wires too, since the remaining cycles all run through a repository rather than a service. It was rejected: it rewrites the signatures of all four context setup functions to catch one setter per service, and the guarded setters below already make that setter loud. It remains the obvious next move if a fifth cycle ever appears.

## Consequences

Four `Wire…(x) error` calls survive, and they are not oversights. Each is a real cycle:

- `PostContext.WireFeedEventEmitter` and `UserContext.WireFeedEventEmitter` — the dispatcher's fanout worker reads posts, so the feed context cannot exist before the post and follow services.
- `UserContext.WireNotificationEmitter` — the notification context is built from the user repository that `SetupUserContext` creates alongside the follow service.
- `SuppressionGate.WireChecker` — the mailer is built during infrastructure setup, before the user context that owns the suppression table.

All four refuse a nil and refuse a second call. The second refusal is not symmetry: these fields are read by concurrent requests without synchronisation, so writing one once during setup is safe while writing it again after serving starts is a data race.
