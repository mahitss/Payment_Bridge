package payouts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/paypal"
)

var (
	ErrWebhookUnconfigured = errors.New("paypal webhook integration is not configured")
	ErrWebhookIgnored      = errors.New("webhook event ignored (unmatched or fixture protected)")
)

// SetWebhookVerifier configures a custom webhook verifier (e.g. For mock testing).
func (s *Service) SetWebhookVerifier(v *paypal.WebhookVerifier) {
	s.webhookVerifier = v
}

// GetWebhookVerifier returns the current webhook verifier, defaulting to a production verifier.
func (s *Service) GetWebhookVerifier() *paypal.WebhookVerifier {
	if s.webhookVerifier == nil {
		s.webhookVerifier = paypal.NewWebhookVerifier()
	}
	return s.webhookVerifier
}

// WebhookProcessResult contains details about the processed webhook outcome.
type WebhookProcessResult struct {
	Event            *db.PayPalWebhookEvent
	IsDuplicate      bool
	MatchedOperation *db.PayoutOperation
}

// ProcessPayPalWebhook cryptographically verifies, durably stores, and authoritatively reconciles
// an incoming PayPal webhook payload against ClaimBridge payout operations.
func (s *Service) ProcessPayPalWebhook(
	ctx context.Context,
	headers paypal.WebhookHeaders,
	rawBody []byte,
) (*WebhookProcessResult, error) {
	// 1. Enforce Webhook ID Configuration
	webhookID := strings.TrimSpace(s.cfg.PayPalWebhookID)
	if webhookID == "" {
		slog.WarnContext(ctx, "PayPal webhook received but PAYPAL_WEBHOOK_ID is unconfigured")
		return nil, ErrWebhookUnconfigured
	}

	// 2. Cryptographic Signature Verification (Fail-Closed)
	verifier := s.GetWebhookVerifier()
	sigErr := verifier.Verify(ctx, headers, webhookID, rawBody)
	if sigErr != nil {
		slog.WarnContext(ctx, "PayPal webhook signature verification failed",
			slog.String("error", sigErr.Error()),
			slog.String("transmission_id", headers.TransmissionID),
			slog.String("cert_url", headers.CertURL),
		)

		// Record failed signature attempt into audit log if parseable
		var partialEvent paypal.WebhookEvent
		_ = json.Unmarshal(rawBody, &partialEvent)

		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			EventType: "PAYOUT_WEBHOOK_SIGNATURE_FAILED",
			Metadata: map[string]interface{}{
				"transmission_id":   headers.TransmissionID,
				"transmission_time": headers.TransmissionTime,
				"cert_url":          headers.CertURL,
				"auth_algo":         headers.AuthAlgo,
				"event_id":          partialEvent.ID,
				"event_type":        partialEvent.EventType,
				"error":             sigErr.Error(),
			},
		})

		// Durably record unverified attempt if event ID is present
		if partialEvent.ID != "" {
			var snapshot map[string]interface{}
			_ = json.Unmarshal(rawBody, &snapshot)
			errMsg := sigErr.Error()
			_ = s.repo.CreateWebhookEvent(ctx, &db.PayPalWebhookEvent{
				PayPalEventID:     partialEvent.ID,
				EventType:         partialEvent.EventType,
				ResourceType:      partialEvent.ResourceType,
				Summary:           partialEvent.Summary,
				TransmissionID:    headers.TransmissionID,
				WebhookID:         webhookID,
				SignatureVerified: false,
				ProcessingStatus:  "FAILED_SIGNATURE",
				ProcessingError:   &errMsg,
				PayloadSnapshot:   snapshot,
			})
		}

		return nil, sigErr
	}

	// 3. Parse and Validate Envelope
	var event paypal.WebhookEvent
	if err := json.Unmarshal(rawBody, &event); err != nil {
		return nil, fmt.Errorf("malformed webhook JSON payload: %w", err)
	}
	if strings.TrimSpace(event.ID) == "" {
		return nil, errors.New("webhook event missing required id")
	}
	if strings.TrimSpace(event.EventType) == "" {
		return nil, errors.New("webhook event missing required event_type")
	}

	// 4. Idempotency Check & Crash Recovery
	var webhookRecord *db.PayPalWebhookEvent
	isDuplicate := false
	existing, err := s.repo.GetWebhookEventByPayPalEventID(ctx, event.ID)
	if err == nil && existing != nil {
		isDuplicate = true
		if existing.ProcessingStatus == "PROCESSED" {
			slog.InfoContext(ctx, "Duplicate PayPal webhook delivery ignored (already PROCESSED)",
				slog.String("paypal_event_id", event.ID),
				slog.String("event_type", event.EventType),
				slog.String("processing_status", existing.ProcessingStatus),
			)

			_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
				OrganizationID: existing.OrganizationID,
				EventType:      "PAYOUT_WEBHOOK_DUPLICATE_DELIVERY",
				Metadata: map[string]interface{}{
					"paypal_event_id":   event.ID,
					"event_type":        event.EventType,
					"prior_status":      existing.ProcessingStatus,
					"prior_recorded_at": existing.CreatedAt,
				},
			})

			return &WebhookProcessResult{
				Event:       existing,
				IsDuplicate: true,
			}, nil
		}

		// Existing event was recorded as RECEIVED or FAILED, meaning server crashed or prior attempt failed.
		// Resume and complete business processing for this event!
		slog.WarnContext(ctx, "Resuming unprocessed/interrupted PayPal webhook event after prior crash",
			slog.String("paypal_event_id", event.ID),
			slog.String("prior_status", existing.ProcessingStatus),
		)
		webhookRecord = existing
	} else {
		// 5. Initial Durable Persistence (Status = RECEIVED)
		var rawSnapshot map[string]interface{}
		_ = json.Unmarshal(rawBody, &rawSnapshot)

		var transmissionTimePtr *time.Time
		if t, parseErr := time.Parse(time.RFC3339, headers.TransmissionTime); parseErr == nil {
			transmissionTimePtr = &t
		}

		webhookRecord = &db.PayPalWebhookEvent{
			PayPalEventID:     event.ID,
			EventType:         event.EventType,
			ResourceType:      event.ResourceType,
			Summary:           event.Summary,
			TransmissionID:    headers.TransmissionID,
			TransmissionTime:  transmissionTimePtr,
			WebhookID:         webhookID,
			SignatureVerified: true,
			ProcessingStatus:  "RECEIVED",
			PayloadSnapshot:   rawSnapshot,
		}

		if err := s.repo.CreateWebhookEvent(ctx, webhookRecord); err != nil {
			if errors.Is(err, db.ErrWebhookEventAlreadyExists) {
				isDuplicate = true
				// Concurrent duplicate delivery caught by DB unique constraint
				dup, getErr := s.repo.GetWebhookEventByPayPalEventID(ctx, event.ID)
				if getErr == nil && dup != nil {
					if dup.ProcessingStatus == "PROCESSED" {
						return &WebhookProcessResult{
							Event:       dup,
							IsDuplicate: true,
						}, nil
					}
					webhookRecord = dup
				} else {
					return nil, fmt.Errorf("failed retrieving concurrent duplicate webhook event: %w", err)
				}
			} else {
				return nil, fmt.Errorf("failed to durably record webhook event: %w", err)
			}
		}
	}

	// 6. Authoritative Reconciliation
	op, reconErr := s.reconcileWebhookEvent(ctx, &event, webhookRecord)
	now := time.Now().UTC()
	if reconErr != nil {
		errMsg := reconErr.Error()
		webhookRecord.ProcessingStatus = "FAILED"
		webhookRecord.ProcessingError = &errMsg
		webhookRecord.ProcessedAt = &now
		_ = s.repo.UpdateWebhookEventStatus(ctx, webhookRecord.ID, "FAILED", &errMsg, &now, webhookRecord.OrganizationID, webhookRecord.PayoutOperationID)
		return &WebhookProcessResult{
			Event:            webhookRecord,
			MatchedOperation: op,
		}, reconErr
	}

	// 7. Update Event Processing Status
	webhookRecord.ProcessedAt = &now
	_ = s.repo.UpdateWebhookEventStatus(
		ctx,
		webhookRecord.ID,
		webhookRecord.ProcessingStatus,
		webhookRecord.ProcessingError,
		&now,
		webhookRecord.OrganizationID,
		webhookRecord.PayoutOperationID,
	)

	return &WebhookProcessResult{
		Event:            webhookRecord,
		MatchedOperation: op,
		IsDuplicate:      isDuplicate,
	}, nil
}

// reconcileWebhookEvent performs provider correlation and state machine reconciliation.
func (s *Service) reconcileWebhookEvent(
	ctx context.Context,
	event *paypal.WebhookEvent,
	record *db.PayPalWebhookEvent,
) (*db.PayoutOperation, error) {
	// Extract resource identifiers
	var (
		batchID       string
		itemID        string
		senderBatchID string
		senderItemID  string
	)

	if paypal.IsBatchEvent(event.EventType) {
		batchResp, err := event.ParsePayoutBatch()
		if err == nil && batchResp != nil {
			batchID = batchResp.BatchHeader.PayoutBatchID
			senderBatchID = batchResp.BatchHeader.SenderBatchID
		}
	} else if paypal.IsItemEvent(event.EventType) {
		item, bID, err := event.ParsePayoutItem()
		if err == nil && item != nil {
			itemID = item.PayoutItemID
			senderItemID = item.SenderItemID
			batchID = bID
		}
	} else {
		// For unknown or custom event types, perform best-effort extraction of provider identifiers
		var genericRes struct {
			PayoutBatchID string `json:"payout_batch_id"`
			PayoutItemID  string `json:"payout_item_id"`
			SenderBatchID string `json:"sender_batch_id"`
			SenderItemID  string `json:"sender_item_id"`
		}
		if err := json.Unmarshal(event.Resource, &genericRes); err == nil {
			batchID = genericRes.PayoutBatchID
			itemID = genericRes.PayoutItemID
			senderBatchID = genericRes.SenderBatchID
			senderItemID = genericRes.SenderItemID
		}
	}

	if batchID != "" {
		record.PayoutBatchID = &batchID
	}
	if itemID != "" {
		record.PayoutItemID = &itemID
	}

	// Correlate with database PayoutOperation
	op, err := s.repo.GetPayoutOperationByPayPalIDs(ctx, batchID, itemID, senderBatchID, senderItemID)
	if err != nil {
		if errors.Is(err, db.ErrPayoutOperationNotFound) {
			slog.InfoContext(ctx, "PayPal webhook received with no matching local payout operation",
				slog.String("event_type", event.EventType),
				slog.String("batch_id", batchID),
				slog.String("item_id", itemID),
			)
			errMsg := "no matching payout operation found"
			record.ProcessingStatus = "IGNORED"
			record.ProcessingError = &errMsg

			_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
				EventType: "PAYOUT_WEBHOOK_UNMATCHED",
				Metadata: map[string]interface{}{
					"paypal_event_id": event.ID,
					"event_type":      event.EventType,
					"batch_id":        batchID,
					"item_id":         itemID,
				},
			})
			return nil, nil
		}
		return nil, fmt.Errorf("failed searching for payout operation: %w", err)
	}

	record.OrganizationID = &op.OrganizationID
	record.PayoutOperationID = &op.ID

	// Synthetic Demo Fixture Protection: Never mutate synthetic fixture state via external webhooks
	if op.IsSyntheticFixture {
		slog.WarnContext(ctx, "External webhook rejected on synthetic demo fixture",
			slog.String("operation_id", op.ID),
			slog.String("event_id", event.ID),
		)
		errMsg := "synthetic demo fixture protected from external webhook events"
		record.ProcessingStatus = "IGNORED"
		record.ProcessingError = &errMsg

		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			OrganizationID: &op.OrganizationID,
			EventType:      "PAYOUT_WEBHOOK_FIXTURE_PROTECTION_BLOCKED",
			Metadata: map[string]interface{}{
				"operation_id":    op.ID,
				"paypal_event_id": event.ID,
				"event_type":      event.EventType,
			},
		})
		return op, nil
	}

	// -------------------------------------------------------------------------
	// Terminal State Protection & Out-Of-Order Event Handling
	// -------------------------------------------------------------------------
	if op.Status == "SUCCEEDED" {
		switch event.EventType {
		case paypal.EventPayoutsItemSucceeded, paypal.EventPayoutsBatchSuccess:
			// Reaffirmation of terminal success
			if event.EventType == paypal.EventPayoutsBatchSuccess {
				successStatus := "SUCCESS"
				op.PayPalBatchStatus = &successStatus
				_ = s.repo.UpdatePayoutOperation(ctx, op)
			}
			record.ProcessingStatus = "PROCESSED"
			return op, nil

		case paypal.EventPayoutsBatchProcessing, paypal.EventPayoutsItemUnclaimed:
			// Out-of-order or delayed earlier lifecycle event: do NOT revert settled SUCCEEDED state!
			slog.WarnContext(ctx, "Out-of-order/stale webhook event received for already SUCCEEDED payout operation",
				slog.String("operation_id", op.ID),
				slog.String("event_type", event.EventType),
			)
			_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
				OrganizationID: &op.OrganizationID,
				EventType:      "PAYOUT_WEBHOOK_STALE_EVENT_IGNORED",
				Metadata: map[string]interface{}{
					"operation_id":    op.ID,
					"paypal_event_id": event.ID,
					"event_type":      event.EventType,
					"current_status":  op.Status,
				},
			})
			record.ProcessingStatus = "PROCESSED"
			return op, nil

		case paypal.EventPayoutsItemFailed, paypal.EventPayoutsItemBlocked, paypal.EventPayoutsBatchDenied:
			// Severe provider discrepancy: failure notification after SUCCEEDED!
			// Never overwrite settled financial records automatically; transition to RECONCILIATION_REQUIRED
			slog.ErrorContext(ctx, "CRITICAL: Provider reported failure event on already SUCCEEDED payout operation",
				slog.String("operation_id", op.ID),
				slog.String("event_type", event.EventType),
			)
			op.Status = "RECONCILIATION_REQUIRED"
			errMsg := fmt.Sprintf("Critical discrepancy: received %s for already SUCCEEDED operation; manual review required", event.EventType)
			op.ErrorMessage = &errMsg
			_ = s.repo.UpdatePayoutOperation(ctx, op)

			_ = s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
				PayoutOperationID: op.ID,
				OrganizationID:    op.OrganizationID,
				Provider:          "PAYPAL",
				BatchStatus:       event.EventType,
				ItemStatus:        op.PayPalItemStatus,
				RawResponse:       record.PayloadSnapshot,
			})

			_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
				OrganizationID: &op.OrganizationID,
				EventType:      "PAYOUT_WEBHOOK_TERMINAL_CONFLICT",
				Metadata: map[string]interface{}{
					"operation_id":    op.ID,
					"paypal_event_id": event.ID,
					"event_type":      event.EventType,
				},
			})
			record.ProcessingStatus = "PROCESSED"
			return op, nil
		}
	}

	if op.Status == "CANCELLED" && event.EventType == paypal.EventPayoutsItemSucceeded {
		// Payout was marked cancelled locally, but provider reports payment succeeded!
		slog.ErrorContext(ctx, "CRITICAL: Payout cancelled locally but PayPal reports item SUCCEEDED",
			slog.String("operation_id", op.ID),
			slog.String("event_type", event.EventType),
		)
		op.Status = "RECONCILIATION_REQUIRED"
		errMsg := "Payout was cancelled locally, but provider reports item SUCCEEDED"
		op.ErrorMessage = &errMsg
		_ = s.repo.UpdatePayoutOperation(ctx, op)
		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			OrganizationID: &op.OrganizationID,
			EventType:      "PAYOUT_WEBHOOK_TERMINAL_CONFLICT",
			Metadata: map[string]interface{}{
				"operation_id":    op.ID,
				"paypal_event_id": event.ID,
				"event_type":      event.EventType,
			},
		})
		record.ProcessingStatus = "PROCESSED"
		return op, nil
	}

	if op.Status == "FAILED" && event.EventType == paypal.EventPayoutsItemSucceeded {
		// Recovery from failure: provider actually succeeded!
		item, _, err := event.ParsePayoutItem()
		if err == nil && item != nil {
			if op.PayPalPayoutItemID == nil && item.PayoutItemID != "" {
				op.PayPalPayoutItemID = &item.PayoutItemID
			}
			itemSuccessStatus := "SUCCESS"
			op.PayPalItemStatus = &itemSuccessStatus
			op.Status = "SUCCEEDED"
			now := time.Now().UTC()
			op.CompletedAt = &now
			op.ErrorMessage = nil

			_ = s.repo.UpdatePayoutCaseStatus(ctx, op.OrganizationID, op.PayoutCaseID, "COMPLETED")
			_ = s.repo.UpdatePayoutOperation(ctx, op)
			_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
				OrganizationID: &op.OrganizationID,
				EventType:      "PAYOUT_WEBHOOK_RECOVERED_FROM_FAILURE",
				Metadata: map[string]interface{}{
					"operation_id":    op.ID,
					"paypal_event_id": event.ID,
				},
			})
			record.ProcessingStatus = "PROCESSED"
			return op, nil
		}
	}

	// Execute Event-Specific State Transitions
	switch event.EventType {

	// -------------------------------------------------------------------------
	// Batch Events
	// -------------------------------------------------------------------------
	case paypal.EventPayoutsBatchDenied:
		batchResp, err := event.ParsePayoutBatch()
		if err != nil {
			return op, fmt.Errorf("failed parsing batch resource: %w", err)
		}
		op.Status = "FAILED"
		deniedStatus := "DENIED"
		op.PayPalBatchStatus = &deniedStatus
		errMsg := "PayPal payout batch was denied"
		op.ErrorMessage = &errMsg

		if err := s.repo.UpdatePayoutCaseStatus(ctx, op.OrganizationID, op.PayoutCaseID, "DENIED"); err != nil {
			return op, fmt.Errorf("failed updating payout case status to DENIED: %w", err)
		}
		if err := s.repo.UpdatePayoutOperation(ctx, op); err != nil {
			return op, fmt.Errorf("failed updating payout operation to FAILED: %w", err)
		}

		_ = s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
			PayoutOperationID: op.ID,
			OrganizationID:    op.OrganizationID,
			Provider:          "PAYPAL",
			BatchStatus:       "DENIED",
			ItemStatus:        op.PayPalItemStatus,
			RawResponse:       record.PayloadSnapshot,
		})

		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			OrganizationID: &op.OrganizationID,
			EventType:      "PAYOUT_WEBHOOK_BATCH_DENIED",
			Metadata: map[string]interface{}{
				"operation_id":    op.ID,
				"paypal_event_id": event.ID,
				"batch_id":        batchID,
				"reason":          batchResp.BatchHeader.BatchStatus,
			},
		})

	case paypal.EventPayoutsBatchProcessing:
		procStatus := "PROCESSING"
		op.PayPalBatchStatus = &procStatus
		if op.Status == "PREPARED" || op.Status == "APPROVED" || op.Status == "SUBMITTING" {
			op.Status = "SUBMITTED"
		}
		if err := s.repo.UpdatePayoutOperation(ctx, op); err != nil {
			return op, fmt.Errorf("failed updating operation: %w", err)
		}

		_ = s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
			PayoutOperationID: op.ID,
			OrganizationID:    op.OrganizationID,
			Provider:          "PAYPAL",
			BatchStatus:       "PROCESSING",
			ItemStatus:        op.PayPalItemStatus,
			RawResponse:       record.PayloadSnapshot,
		})

		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			OrganizationID: &op.OrganizationID,
			EventType:      "PAYOUT_WEBHOOK_BATCH_PROCESSING",
			Metadata: map[string]interface{}{
				"operation_id":    op.ID,
				"paypal_event_id": event.ID,
				"batch_id":        batchID,
			},
		})

	case paypal.EventPayoutsBatchSuccess:
		// CRITICAL INVARIANT: Batch SUCCESS alone does not prove individual payout completion!
		// Completion is strictly item-authoritative.
		batchResp, err := event.ParsePayoutBatch()
		if err != nil {
			return op, fmt.Errorf("failed parsing batch success resource: %w", err)
		}

		successStatus := "SUCCESS"
		op.PayPalBatchStatus = &successStatus

		// If items are included in the batch resource, reconcile authoritatively
		if len(batchResp.Items) > 0 {
			_, updatedOp, err := s.reconcileBatchResponse(
				ctx,
				nil,
				op.OrganizationID,
				op.PayoutCaseID,
				op,
				batchResp,
				"PAYOUT_WEBHOOK_BATCH_RECONCILED",
				map[string]interface{}{"paypal_event_id": event.ID},
				"",
			)
			if err != nil {
				return op, fmt.Errorf("failed reconciling batch response: %w", err)
			}
			op = updatedOp
		} else {
			// Item details are omitted in notification; keep SUBMITTED until item evidence is established
			if op.Status == "PREPARED" || op.Status == "APPROVED" || op.Status == "SUBMITTING" {
				op.Status = "SUBMITTED"
			}
			if err := s.repo.UpdatePayoutOperation(ctx, op); err != nil {
				return op, fmt.Errorf("failed updating operation status: %w", err)
			}

			_ = s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
				PayoutOperationID: op.ID,
				OrganizationID:    op.OrganizationID,
				Provider:          "PAYPAL",
				BatchStatus:       "SUCCESS",
				ItemStatus:        op.PayPalItemStatus,
				RawResponse:       record.PayloadSnapshot,
			})

			_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
				OrganizationID: &op.OrganizationID,
				EventType:      "PAYOUT_WEBHOOK_BATCH_SUCCESS_ITEM_PENDING",
				Metadata: map[string]interface{}{
					"operation_id":    op.ID,
					"paypal_event_id": event.ID,
					"batch_id":        batchID,
					"note":            "batch reports SUCCESS but item evidence is pending authoritative lookup",
				},
			})

			// If PayPal client is configured, execute authoritative sync to retrieve item evidence
			if s.paypalClient != nil && op.PayPalPayoutBatchID != nil {
				_, syncedOp, syncErr := s.SyncPayoutCase(ctx, op.OrganizationID, op.PayoutCaseID)
				if syncErr == nil && syncedOp != nil {
					op = syncedOp
				}
			}
		}

	// -------------------------------------------------------------------------
	// Item Events (Authoritative Recipient Evidence)
	// -------------------------------------------------------------------------
	case paypal.EventPayoutsItemSucceeded:
		item, _, err := event.ParsePayoutItem()
		if err != nil {
			return op, fmt.Errorf("failed parsing item resource: %w", err)
		}

		if op.PayPalPayoutItemID == nil && item.PayoutItemID != "" {
			op.PayPalPayoutItemID = &item.PayoutItemID
		}
		itemSuccessStatus := "SUCCESS"
		op.PayPalItemStatus = &itemSuccessStatus
		op.Status = "SUCCEEDED"
		now := time.Now().UTC()
		op.CompletedAt = &now
		op.ErrorMessage = nil

		if err := s.repo.UpdatePayoutCaseStatus(ctx, op.OrganizationID, op.PayoutCaseID, "COMPLETED"); err != nil {
			return op, fmt.Errorf("failed marking payout case COMPLETED: %w", err)
		}
		if err := s.repo.UpdatePayoutOperation(ctx, op); err != nil {
			return op, fmt.Errorf("failed updating operation to SUCCEEDED: %w", err)
		}

		batchStat := "SUCCESS"
		if op.PayPalBatchStatus != nil && *op.PayPalBatchStatus != "" {
			batchStat = *op.PayPalBatchStatus
		}
		_ = s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
			PayoutOperationID: op.ID,
			OrganizationID:    op.OrganizationID,
			Provider:          "PAYPAL",
			BatchStatus:       batchStat,
			ItemStatus:        &itemSuccessStatus,
			RawResponse:       record.PayloadSnapshot,
		})

		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			OrganizationID: &op.OrganizationID,
			EventType:      "PAYOUT_WEBHOOK_ITEM_SUCCEEDED",
			Metadata: map[string]interface{}{
				"operation_id":    op.ID,
				"paypal_event_id": event.ID,
				"item_id":         item.PayoutItemID,
				"batch_id":        batchID,
			},
		})

	case paypal.EventPayoutsItemFailed, paypal.EventPayoutsItemBlocked:
		item, _, err := event.ParsePayoutItem()
		if err != nil {
			return op, fmt.Errorf("failed parsing item resource: %w", err)
		}

		if op.PayPalPayoutItemID == nil && item.PayoutItemID != "" {
			op.PayPalPayoutItemID = &item.PayoutItemID
		}
		statusStr := item.TransactionStatus
		if statusStr == "" {
			statusStr = "FAILED"
		}
		op.PayPalItemStatus = &statusStr
		op.Status = "FAILED"
		errMsg := fmt.Sprintf("PayPal payout item status: %s", statusStr)
		op.ErrorMessage = &errMsg

		if err := s.repo.UpdatePayoutCaseStatus(ctx, op.OrganizationID, op.PayoutCaseID, "FAILED"); err != nil {
			return op, fmt.Errorf("failed marking payout case FAILED: %w", err)
		}
		if err := s.repo.UpdatePayoutOperation(ctx, op); err != nil {
			return op, fmt.Errorf("failed updating operation to FAILED: %w", err)
		}

		batchStat := "PROCESSING"
		if op.PayPalBatchStatus != nil {
			batchStat = *op.PayPalBatchStatus
		}
		_ = s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
			PayoutOperationID: op.ID,
			OrganizationID:    op.OrganizationID,
			Provider:          "PAYPAL",
			BatchStatus:       batchStat,
			ItemStatus:        &statusStr,
			RawResponse:       record.PayloadSnapshot,
		})

		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			OrganizationID: &op.OrganizationID,
			EventType:      "PAYOUT_WEBHOOK_ITEM_" + strings.ToUpper(statusStr),
			Metadata: map[string]interface{}{
				"operation_id":    op.ID,
				"paypal_event_id": event.ID,
				"item_id":         item.PayoutItemID,
				"status":          statusStr,
			},
		})

	case paypal.EventPayoutsItemUnclaimed:
		item, _, err := event.ParsePayoutItem()
		if err != nil {
			return op, fmt.Errorf("failed parsing item resource: %w", err)
		}

		if op.PayPalPayoutItemID == nil && item.PayoutItemID != "" {
			op.PayPalPayoutItemID = &item.PayoutItemID
		}
		unclaimedStatus := "UNCLAIMED"
		op.PayPalItemStatus = &unclaimedStatus
		op.Status = "SUBMITTED"

		if err := s.repo.UpdatePayoutOperation(ctx, op); err != nil {
			return op, fmt.Errorf("failed updating operation: %w", err)
		}

		batchStat := "PROCESSING"
		if op.PayPalBatchStatus != nil {
			batchStat = *op.PayPalBatchStatus
		}
		_ = s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
			PayoutOperationID: op.ID,
			OrganizationID:    op.OrganizationID,
			Provider:          "PAYPAL",
			BatchStatus:       batchStat,
			ItemStatus:        &unclaimedStatus,
			RawResponse:       record.PayloadSnapshot,
		})

		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			OrganizationID: &op.OrganizationID,
			EventType:      "PAYOUT_WEBHOOK_ITEM_UNCLAIMED",
			Metadata: map[string]interface{}{
				"operation_id":    op.ID,
				"paypal_event_id": event.ID,
				"item_id":         item.PayoutItemID,
			},
		})

	case paypal.EventPayoutsItemHeld, paypal.EventPayoutsItemRefunded:
		item, _, err := event.ParsePayoutItem()
		if err != nil {
			return op, fmt.Errorf("failed parsing item resource: %w", err)
		}

		if op.PayPalPayoutItemID == nil && item.PayoutItemID != "" {
			op.PayPalPayoutItemID = &item.PayoutItemID
		}
		statusStr := item.TransactionStatus
		if statusStr == "" {
			statusStr = "HELD"
		}
		op.PayPalItemStatus = &statusStr
		op.Status = "RECONCILIATION_REQUIRED"
		errMsg := fmt.Sprintf("Payout item placed in %s state; manual reconciliation required", statusStr)
		op.ErrorMessage = &errMsg

		if err := s.repo.UpdatePayoutOperation(ctx, op); err != nil {
			return op, fmt.Errorf("failed updating operation: %w", err)
		}

		batchStat := "PROCESSING"
		if op.PayPalBatchStatus != nil {
			batchStat = *op.PayPalBatchStatus
		}
		_ = s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
			PayoutOperationID: op.ID,
			OrganizationID:    op.OrganizationID,
			Provider:          "PAYPAL",
			BatchStatus:       batchStat,
			ItemStatus:        &statusStr,
			RawResponse:       record.PayloadSnapshot,
		})

		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			OrganizationID: &op.OrganizationID,
			EventType:      "PAYOUT_WEBHOOK_ITEM_RECONCILIATION_REQUIRED",
			Metadata: map[string]interface{}{
				"operation_id":    op.ID,
				"paypal_event_id": event.ID,
				"item_id":         item.PayoutItemID,
				"status":          statusStr,
			},
		})

	case paypal.EventPayoutsItemReturned, paypal.EventPayoutsItemCanceled:
		item, _, err := event.ParsePayoutItem()
		if err != nil {
			return op, fmt.Errorf("failed parsing item resource: %w", err)
		}

		if op.PayPalPayoutItemID == nil && item.PayoutItemID != "" {
			op.PayPalPayoutItemID = &item.PayoutItemID
		}
		statusStr := item.TransactionStatus
		if statusStr == "" {
			statusStr = "CANCELED"
		}
		op.PayPalItemStatus = &statusStr

		if op.Status == "CANCELLED" {
			// Preserve already-recorded local cancellation initiated by ClaimBridge
		} else {
			op.Status = "RECONCILIATION_REQUIRED"
			errMsg := fmt.Sprintf("Provider reported item %s without local cancellation record", statusStr)
			op.ErrorMessage = &errMsg
		}

		if err := s.repo.UpdatePayoutOperation(ctx, op); err != nil {
			return op, fmt.Errorf("failed updating operation: %w", err)
		}

		batchStat := "PROCESSING"
		if op.PayPalBatchStatus != nil {
			batchStat = *op.PayPalBatchStatus
		}
		_ = s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
			PayoutOperationID: op.ID,
			OrganizationID:    op.OrganizationID,
			Provider:          "PAYPAL",
			BatchStatus:       batchStat,
			ItemStatus:        &statusStr,
			RawResponse:       record.PayloadSnapshot,
		})

		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			OrganizationID: &op.OrganizationID,
			EventType:      "PAYOUT_WEBHOOK_ITEM_" + strings.ToUpper(statusStr),
			Metadata: map[string]interface{}{
				"operation_id":    op.ID,
				"paypal_event_id": event.ID,
				"item_id":         item.PayoutItemID,
				"status":          statusStr,
			},
		})

	default:
		// Unknown event type: safe handling without corrupting payout state
		slog.InfoContext(ctx, "Unknown PayPal webhook event received; safely recorded without financial mutation",
			slog.String("event_type", event.EventType),
			slog.String("event_id", event.ID),
		)

		_ = s.repo.CreatePayoutStatusSnapshot(ctx, &db.PayoutStatusSnapshot{
			PayoutOperationID: op.ID,
			OrganizationID:    op.OrganizationID,
			Provider:          "PAYPAL",
			BatchStatus:       event.EventType,
			RawResponse:       record.PayloadSnapshot,
		})

		_ = s.repo.RecordAuditEvent(ctx, &db.AuditEvent{
			OrganizationID: &op.OrganizationID,
			EventType:      "PAYOUT_WEBHOOK_UNKNOWN_EVENT_RECORDED",
			Metadata: map[string]interface{}{
				"operation_id":    op.ID,
				"paypal_event_id": event.ID,
				"event_type":      event.EventType,
			},
		})
	}

	record.ProcessingStatus = "PROCESSED"
	return op, nil
}

// RecoverPendingWebhookEvents processes any events previously stored in RECEIVED or FAILED status
// that were interrupted by a server restart or network timeout.
func (s *Service) RecoverPendingWebhookEvents(ctx context.Context, olderThan time.Duration, maxEvents int) (int, error) {
	events, err := s.repo.ListPendingWebhookEvents(ctx, olderThan, maxEvents)
	if err != nil {
		return 0, fmt.Errorf("failed listing pending webhook events: %w", err)
	}

	recoveredCount := 0
	for i := range events {
		rec := &events[i]
		rawBytes, mErr := json.Marshal(rec.PayloadSnapshot)
		if mErr != nil {
			continue
		}
		var evt paypal.WebhookEvent
		if uErr := json.Unmarshal(rawBytes, &evt); uErr != nil {
			continue
		}

		op, rErr := s.reconcileWebhookEvent(ctx, &evt, rec)
		now := time.Now().UTC()
		if rErr != nil {
			errMsg := rErr.Error()
			_ = s.repo.UpdateWebhookEventStatus(ctx, rec.ID, "FAILED", &errMsg, &now, rec.OrganizationID, rec.PayoutOperationID)
			continue
		}

		opID := rec.PayoutOperationID
		orgID := rec.OrganizationID
		if op != nil {
			opID = &op.ID
			orgID = &op.OrganizationID
		}
		_ = s.repo.UpdateWebhookEventStatus(ctx, rec.ID, "PROCESSED", nil, &now, orgID, opID)
		recoveredCount++
	}

	return recoveredCount, nil
}
