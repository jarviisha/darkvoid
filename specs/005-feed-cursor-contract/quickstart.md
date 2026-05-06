# Quickstart: Feed Cursor Contract

## Goal

Verify that `/feed` emits and accepts the no-version cursor contract and rejects obsolete cursor shapes.

## Preconditions

- Run local dependencies if needed with `make docker-up`.
- Configure an authenticated test user with enough feed candidates to produce multiple pages.
- Ensure the user has primary timeline content and at least one supplemental source available for contract verification.

## Verification Steps

1. Run targeted feed tests:

   ```bash
   go test ./internal/feature/feed/...
   ```

2. Request the first feed page with no cursor.

   Expected:
   - HTTP `200`
   - response has `data`
   - response has `next_cursor` when additional items exist
   - decoded logical cursor contains no `v`
   - decoded logical cursor uses flat fields such as `tl_score`, `tl_post_id`, `tl_user`, `rec_offset`, `trend_score`, and `trend_post_id` when those sources remain active

3. Request the next feed page using the returned `next_cursor`.

   Expected:
   - HTTP `200`
   - no repeated post IDs from the previous page
   - subsequent `next_cursor` advances active source positions

4. Send obsolete cursor shapes.

   Expected invalid cursor response:
   - any cursor containing `v`
   - any cursor containing nested `timeline`
   - any old session-style cursor
   - any cursor with negative numeric positions
   - any cursor with mismatched `tl_user`

5. Confirm `/discover` still paginates with its existing cursor contract.

6. If handler Swagger comments changed, regenerate documentation:

   ```bash
   make swagger-generate
   ```

7. Run full validation before merge:

   ```bash
   make test
   make lint
   ```
