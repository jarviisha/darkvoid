# Service Deps take concrete repositories

The repository ports declare transactional variants of themselves — `postRepo` requires `WithTx(pgx.Tx) postRepo` — which the sqlc-generated repositories do not satisfy, since their own `WithTx` returns the concrete type. Only the unexported `*postRepoTxable` wrappers do. `PostDeps` and its siblings therefore take the concrete `*repository.PostRepository` and wrap it inside the constructor.

## Consequences

The constructors cannot be handed a mock, so service tests build the struct directly and `NewPostService` and its siblings are exercised only by their dependency-validation tests. This looks like an oversight and is not one.

Moving the wrapping to the composition root would make the constructors fully mockable, at the cost of putting transaction plumbing in the wiring layer and exporting wrappers that exist only to satisfy a port's shape. That trade was considered and rejected; reopen it if the service constructors ever grow logic worth testing beyond validation.
