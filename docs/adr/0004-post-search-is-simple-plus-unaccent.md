# Post search indexes on 'simple' plus unaccent, and pays for an IMMUTABLE wrapper to do it

`post.posts.search_vector` is generated as `to_tsvector('simple', post.immutable_unaccent(coalesce(content, '')))`, and `SearchPosts` builds its `plainto_tsquery` the same way. The two are halves of one comparison and have to be read together: a query built on a different configuration does not match worse, it matches nothing, and an empty result page is indistinguishable from an empty corpus.

The column was `'english'` until migration 000015. That was never a choice between two right answers — the corpus is Vietnamese, Postgres ships no Vietnamese configuration, and `'english'` was the template default nobody revisited. It cost two things. Vietnamese words that spell like English stopwords were dropped from the index entirely: `to` (big), `an` (eat), `do` (because), `no` (it), `so` (compare), `in` (print), `than` (coal) all produced an empty vector, so no query could reach them regardless of how the query was written. And a search typed without diacritics matched nothing at all, because `tiền` and `tien` are different lexemes — measured on the development corpus, the word `tiền` appears in a post that `tien` could not find. Typing Vietnamese without its marks is ordinary input, not an edge case.

`'simple'` keeps every token and does no stemming, which Vietnamese does not need. `unaccent` folds the marks so both halves land on the same lexeme.

## The wrapper is a lie, deliberately

A `STORED` generated column requires an `IMMUTABLE` expression, and neither `unaccent` overload is one. Both the one- and two-argument forms report `pg_proc.provolatile = 's'` on PostgreSQL 16 — including the `regdictionary` form that is often assumed immutable — because the dictionary behind them can be replaced without rewriting whatever depends on it. `post.immutable_unaccent` asserts an immutability the dictionary does not offer.

The consequence is worth stating rather than discovering: if `unaccent.rules` is ever edited, rows already written keep the vector built under the old rules and nothing rebuilds them. The repair is a reindex of the column, not a different wrapper. There is no formulation that both satisfies `STORED` and tracks a mutable dictionary, so the alternatives were a trigger-maintained column with the same staleness and more moving parts, or computing `to_tsvector` at query time and giving up the index. The wrapper is the cheapest of the three.

Both the function and the dictionary inside it are addressed by schema rather than left to `search_path`. The expression is resolved once when the column is defined, and a caller's `search_path` must not be able to steer what a write computes.

## What this migration costs

Changing the expression behind a `STORED` column means dropping and re-adding it, which rewrites `post.posts` under `ACCESS EXCLUSIVE` and rebuilds the GIN index. That is real downtime on a large table, not a one-line edit, and it is why the down migration exists as a genuine reversal rather than a formality — though rolling back re-introduces both failures and requires the query side to go back with it.

## User search is knowingly left inconsistent

`SearchUsersByQuery` and its siblings match with `ILIKE '%…%'` over `username` and `display_name`, backed by a trigram index. They are unaffected by this change and still find nothing for a `display_name` typed without diacritics. That is not an oversight — substring matching and full-text matching are different semantics with different ranking, and folding one of them without deciding what the two surfaces are meant to share would leave a subtler inconsistency than the visible one. The convergence is a separate decision, not a follow-through on this one, and it is tracked as issue #20 rather than left to be rediscovered.

Whoever takes it should know the shape of the cost before choosing: the trigram indexes behind user search are keyed on the bare columns, so folding the column in a query takes them out of the plan. Measured on a 50,000-row table with the same column type and index definition, the folded query falls to a sequential scan and only recovers with a replacement expression index. The query-side edit alone looks complete and returns the right rows.
