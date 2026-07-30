-- name: CreateEmailDelivery :one
INSERT INTO usr.email_deliveries (user_id, provider_message_id, recipient, kind, status)
VALUES ($1, $2, LOWER(sqlc.arg(recipient)), $3, $4)
RETURNING *;

-- name: GetEmailDeliveryByProviderMessageID :one
SELECT * FROM usr.email_deliveries
WHERE provider_message_id = $1;

-- ApplyEmailDeliveryEvent is guarded on last_event_at so a webhook that arrives
-- late cannot overwrite a newer outcome — Resend retries and does not promise
-- ordering, so 'delivered' can land after 'bounced'. Returning the row count is
-- what lets the caller tell "no such send" from "stale event, ignored".
-- name: ApplyEmailDeliveryEvent :execrows
UPDATE usr.email_deliveries
SET status = $2, last_event_at = $3
WHERE provider_message_id = $1
  AND (last_event_at IS NULL OR last_event_at <= $3);

-- name: SuppressEmail :exec
INSERT INTO usr.email_suppressions (email, reason, detail)
VALUES (LOWER(sqlc.arg(email)), $1, $2)
ON CONFLICT (email) DO UPDATE
SET reason = EXCLUDED.reason,
    detail = EXCLUDED.detail;

-- name: IsEmailSuppressed :one
SELECT EXISTS (
    SELECT 1 FROM usr.email_suppressions WHERE email = LOWER(sqlc.arg(email))
) AS suppressed;

-- name: DeleteEmailSuppression :execrows
DELETE FROM usr.email_suppressions
WHERE email = LOWER(sqlc.arg(email));

-- name: ListEmailSuppressions :many
SELECT * FROM usr.email_suppressions
ORDER BY created_at DESC
LIMIT $1;
