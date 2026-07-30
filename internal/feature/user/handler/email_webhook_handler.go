package handler

import (
	"context"
	"errors"
	"io"
	"net/http"

	httputil "github.com/jarviisha/darkvoid/internal/http"
	"github.com/jarviisha/darkvoid/internal/infrastructure/mailer"
	apperrors "github.com/jarviisha/darkvoid/pkg/errors"
	"github.com/jarviisha/darkvoid/pkg/logger"
)

// maxWebhookBody bounds how much of a webhook request body is read.
const maxWebhookBody = 1 << 20 // 1 MiB

// webhookVerifier authenticates a webhook request.
type webhookVerifier interface {
	Verify(header http.Header, body []byte) error
}

// emailEventApplier applies a verified delivery report.
type emailEventApplier interface {
	HandleResendEvent(ctx context.Context, event *mailer.WebhookEvent) error
}

// EmailWebhookHandler receives provider delivery reports.
//
// This route carries no auth middleware: the request comes from Resend, not from
// a user, and the Svix signature is its authentication. That is why the handler
// verifies before it parses, and why the route is not registered at all when no
// webhook secret is configured — an unverified endpoint that writes to the
// suppression list would let anyone block any address.
type EmailWebhookHandler struct {
	verifier webhookVerifier
	events   emailEventApplier
}

// NewEmailWebhookHandler creates a new EmailWebhookHandler.
func NewEmailWebhookHandler(verifier webhookVerifier, events emailEventApplier) *EmailWebhookHandler {
	return &EmailWebhookHandler{verifier: verifier, events: events}
}

// HandleResend godoc
//
//	@Summary		Resend delivery webhook
//	@Description	Receives Resend delivery events (delivered, bounced, complained) and updates the email delivery log. Authenticated by the Svix signature headers, not by a bearer token. Only registered when RESEND_WEBHOOK_SECRET is configured.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Success		200	{object}	httputil.MessageResponse	"Event accepted"
//	@Failure		400	{object}	errors.ErrorResponse		"Malformed payload"
//	@Failure		401	{object}	errors.ErrorResponse		"Signature verification failed"
//	@Failure		500	{object}	errors.ErrorResponse		"Event could not be applied; the provider will retry"
//	@ID				resendWebhook
//	@Router			/webhooks/resend [post]
func (h *EmailWebhookHandler) HandleResend(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// The signature covers the exact bytes received, so the raw body has to be
	// read before anything decodes it.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		apperrors.WriteJSON(w, apperrors.NewBadRequestError("could not read request body"))
		return
	}

	if verifyErr := h.verifier.Verify(r.Header, body); verifyErr != nil {
		logger.Warn(ctx, "rejected email webhook", "error", verifyErr)
		apperrors.WriteJSON(w, apperrors.NewUnauthorizedError("invalid webhook signature"))
		return
	}

	event, err := mailer.ParseResendWebhook(body)
	if err != nil {
		// Signed but unusable: the provider will retry, and it will keep failing,
		// so say so rather than pretending to have accepted it.
		logger.LogError(ctx, err, "could not parse a verified email webhook")
		apperrors.WriteJSON(w, apperrors.NewBadRequestError("malformed webhook payload"))
		return
	}

	if err := h.events.HandleResendEvent(ctx, event); err != nil {
		// 500 on purpose: Svix retries these, and a delivery report lost to a
		// database blip is a suppression we never make.
		logger.LogError(ctx, err, "failed to apply email webhook",
			"type", event.Type, "message_id", event.MessageID)
		apperrors.WriteJSON(w, apperrors.NewInternalError(errors.New("could not apply webhook event")))
		return
	}

	httputil.WriteJSON(w, http.StatusOK, httputil.NewMessageResponse("Event accepted"))
}
