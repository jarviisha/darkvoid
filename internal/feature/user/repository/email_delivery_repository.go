package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jarviisha/darkvoid/internal/feature/user/db"
	"github.com/jarviisha/darkvoid/internal/feature/user/entity"
	"github.com/jarviisha/darkvoid/internal/infrastructure/database"
)

// EmailDeliveryRepository persists the account-mail delivery log and the
// suppression list. Both live here because the webhook that writes one reads the
// other, and neither is useful alone.
type EmailDeliveryRepository struct {
	queries db.Querier
	pool    *pgxpool.Pool
}

// NewEmailDeliveryRepository creates a new EmailDeliveryRepository.
func NewEmailDeliveryRepository(pool *pgxpool.Pool) *EmailDeliveryRepository {
	return &EmailDeliveryRepository{
		queries: db.New(pool),
		pool:    pool,
	}
}

// CreateDelivery records a send under the provider's message id.
func (r *EmailDeliveryRepository) CreateDelivery(
	ctx context.Context,
	userID uuid.UUID,
	providerMessageID, recipient string,
	kind entity.EmailKind,
) (*entity.EmailDelivery, error) {
	row, err := r.queries.CreateEmailDelivery(ctx, db.CreateEmailDeliveryParams{
		UserID:            userID,
		ProviderMessageID: providerMessageID,
		Recipient:         recipient,
		Kind:              string(kind),
		Status:            string(entity.EmailDeliverySent),
	})
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return dbEmailDeliveryToEntity(row), nil
}

// GetDeliveryByProviderMessageID looks up a send by the provider's id.
func (r *EmailDeliveryRepository) GetDeliveryByProviderMessageID(ctx context.Context, providerMessageID string) (*entity.EmailDelivery, error) {
	row, err := r.queries.GetEmailDeliveryByProviderMessageID(ctx, providerMessageID)
	if err != nil {
		return nil, database.MapDBError(err)
	}
	return dbEmailDeliveryToEntity(row), nil
}

// ApplyEvent records a provider outcome against a send and reports whether it
// changed anything. False means either no such send or an event older than the
// one already applied — the query cannot tell those apart, and neither caller
// needs it to.
func (r *EmailDeliveryRepository) ApplyEvent(
	ctx context.Context,
	providerMessageID string,
	status entity.EmailDeliveryStatus,
	occurredAt time.Time,
) (bool, error) {
	rows, err := r.queries.ApplyEmailDeliveryEvent(ctx, db.ApplyEmailDeliveryEventParams{
		ProviderMessageID: providerMessageID,
		Status:            string(status),
		LastEventAt:       pgtype.Timestamptz{Time: occurredAt, Valid: true},
	})
	if err != nil {
		return false, database.MapDBError(err)
	}
	return rows > 0, nil
}

// Suppress adds or updates a suppression entry. Re-suppressing an address is a
// no-op beyond refreshing the reason, which is what makes a replayed webhook safe.
func (r *EmailDeliveryRepository) Suppress(
	ctx context.Context,
	email string,
	reason entity.SuppressionReason,
	detail string,
) error {
	var detailPtr *string
	if detail != "" {
		detailPtr = &detail
	}
	return database.MapDBError(r.queries.SuppressEmail(ctx, db.SuppressEmailParams{
		Email:  email,
		Reason: string(reason),
		Detail: detailPtr,
	}))
}

// IsSuppressed reports whether an address is on the suppression list.
func (r *EmailDeliveryRepository) IsSuppressed(ctx context.Context, email string) (bool, error) {
	suppressed, err := r.queries.IsEmailSuppressed(ctx, email)
	if err != nil {
		return false, database.MapDBError(err)
	}
	return suppressed, nil
}

// Unsuppress removes an address from the suppression list and reports whether it
// was there. Operators need this: a mailbox that was full once would otherwise
// stay unmailable forever.
func (r *EmailDeliveryRepository) Unsuppress(ctx context.Context, email string) (bool, error) {
	rows, err := r.queries.DeleteEmailSuppression(ctx, email)
	if err != nil {
		return false, database.MapDBError(err)
	}
	return rows > 0, nil
}

// ListSuppressions returns the most recently suppressed addresses.
func (r *EmailDeliveryRepository) ListSuppressions(ctx context.Context, limit int32) ([]*entity.EmailSuppression, error) {
	rows, err := r.queries.ListEmailSuppressions(ctx, limit)
	if err != nil {
		return nil, database.MapDBError(err)
	}

	out := make([]*entity.EmailSuppression, 0, len(rows))
	for _, row := range rows {
		s := &entity.EmailSuppression{
			Email:     row.Email,
			Reason:    entity.SuppressionReason(row.Reason),
			CreatedAt: row.CreatedAt.Time,
		}
		if row.Detail != nil {
			s.Detail = *row.Detail
		}
		out = append(out, s)
	}
	return out, nil
}

func dbEmailDeliveryToEntity(row db.UsrEmailDelivery) *entity.EmailDelivery {
	d := &entity.EmailDelivery{
		ID:                row.ID,
		UserID:            row.UserID,
		ProviderMessageID: row.ProviderMessageID,
		Recipient:         row.Recipient,
		Kind:              entity.EmailKind(row.Kind),
		Status:            entity.EmailDeliveryStatus(row.Status),
		CreatedAt:         row.CreatedAt.Time,
	}
	if row.LastEventAt.Valid {
		t := row.LastEventAt.Time
		d.LastEventAt = &t
	}
	return d
}
