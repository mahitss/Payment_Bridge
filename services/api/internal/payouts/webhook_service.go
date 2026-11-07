package payouts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"claimbridge/services/api/internal/crypto"
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

	// 4. Idempotency Check & Atomic Processing Ownership
	var (
		webhookRecord *db.PayPalWebhookEvent
		isDuplicate   = false
		claimToken    = crypto.GenerateUUID()
		leaseDuration = 30 * time.Second
		leaseExpires  = time.Now().UTC().Add(leaseDuration)
	)

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

		// If existing is currently being processed by another active worker, acknowledge duplicate safely
		if existing.ProcessingStatus == "PROCESSING" && existing.LeaseExpiresAt != nil && time.Now().UTC().Before(*existing.LeaseExpiresAt) {
			slog.InfoContext(ctx, "Concurrent PayPal webhook delivery acknowledged (active processing lease held)",
				slog.String("paypal_event_id", event.ID),
				slog.String("event_type", event.EventType),
			)
			return &WebhookProcessResult{
				Event:       existing,
				IsDuplicate: true,
			}, nil
		}

		// Existing event was recorded as RECEIVED, FAILED, or had an expired PROCESSING lease.
		// Atomically acquire exclusive processing lease!
		claimed, claimErr := s.repo.ClaimWebhookEventForProcessing(ctx, existing.ID, claimToken, leaseExpires)
		if claimErr != nil {
			if errors.Is(claimErr, db.ErrWebhookEventAlreadyProcessed) || errors.Is(claimErr, db.ErrWebhookEventActiveClaim) {
				return &WebhookProcessResult{
					Event:       existing,
					IsDuplicate: true,
				}, nil
			}
			return nil, fmt.Errorf("failed acquiring webhook event processing lease: %w", claimErr)
		}
		slog.WarnContext(ctx, "Resuming unprocessed/interrupted PayPal webhook event after acquiring exclusive lease",
			slog.String("paypal_event_id", event.ID),
			slog.String("prior_status", existing.ProcessingStatus),
		)
		webhookRecord = claimed
	} else {
		// 5. Initial Durable Persistence with Atomic Processing Ownership
		var rawSnapshot map[string]interface{}
		_ = json.Unmarshal(rawBody, &rawSnapshot)

		var transmissionTimePtr *time.Time
		if t, parseErr := time.Parse(time.RFC3339, headers.TransmissionTime); parseErr == nil {
			transmissionTimePtr = &t
		}

		webhookRecord = &db.PayPalWebhookEvent{
			PayPalEventID:      event.ID,
			EventType:          event.EventType,
			ResourceType:       event.ResourceType,
			Summary:            event.Summary,
			TransmissionID:     headers.TransmissionID,
			TransmissionTime:   transmissionTimePtr,
			WebhookID:          webhookID,
			SignatureVerified:  true,
			ProcessingStatus:   "PROCESSING",
			ClaimToken:         &claimToken,
			LeaseExpiresAt:     &leaseExpires,
			ProcessingAttempts: 1,
			PayloadSnapshot:    rawSnapshot,
		}

		if createErr := s.repo.CreateWebhookEvent(ctx, webhookRecord); createErr != nil {
			if errors.Is(createErr, db.ErrWebhookEventAlreadyExists) {
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
					claimed, claimErr := s.repo.ClaimWebhookEventForProcessing(ctx, dup.ID, claimToken, leaseExpires)
					if claimErr != nil {
						return &WebhookProcessResult{
							Event:       dup,
							IsDuplicate: true,
						}, nil
					}
					webhookRecord = claimed
				} else {
					return nil, fmt.Errorf("failed retrieving concurrent duplicate webhook event: %w", createErr)
				}
			} else {
				return nil, fmt.Errorf("failed to durably record webhook event: %w", createErr)
			}
		}
	}

	// 6. Authoritative Reconciliation
	op, reconErr := s.reconcileWebhookEvent(ctx, &event, webhookRecord)
	if reconErr != nil {
		errMsg := reconErr.Error()
		attempts := webhookRecord.ProcessingAttempts
		backoff := time.Duration(1<<attempts) * 30 * time.Second
		nextRetry := time.Now().UTC().Add(backoff)
		finalStatus := "FAILED"
		if attempts >= 5 {
			finalStatus = "DEAD_LETTER"
		}
		_ = s.repo.FinalizeWebhookEventProcessing(
			ctx,
			webhookRecord.ID,
			claimToken,
			finalStatus,
			&errMsg,
			&nextRetry,
			webhookRecord.OrganizationID,
			webhookRecord.PayoutOperationID,
		)
		webhookRecord.ProcessingStatus = finalStatus
		webhookRecord.ProcessingError = &errMsg
		return &WebhookProcessResult{
			Event:            webhookRecord,
			MatchedOperation: op,
		}, reconErr
	}

	// 7. Finalize Event Processing Status
	finalStatus := webhookRecord.ProcessingStatus
	if finalStatus == "" || finalStatus == "PROCESSING" {
		finalStatus = "PROCESSED"
	}
	_ = s.repo.FinalizeWebhookEventProcessing(
		ctx,
		webhookRecord.ID,
		claimToken,
		finalStatus,
		webhookRecord.ProcessingError,
		nil,
		webhookRecord.OrganizationID,
		webhookRecord.PayoutOperationID,
	)
	webhookRecord.ProcessingStatus = finalStatus

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
			// PRESERVE AUTHORITATIVE FINANCIAL STATE: Settled payments must NEVER be downgraded
			// to RECONCILIATION_REQUIRED, FAILED, or reopened for resubmission.
			slog.ErrorContext(ctx, "CRITICAL DISCREPANCY: Provider reported failure event on already SUCCEEDED payout operation; preserving financial state and logging incident",
				slog.String("operation_id", op.ID),
				slog.String("event_type", event.EventType),
			)

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
				EventType:      "PAYOUT_WEBHOOK_CONTRADICTORY_EVENT",
				Metadata: map[string]interface{}{
					"operation_id":                  op.ID,
					"paypal_event_id":               event.ID,
					"event_type":                    event.EventType,
					"current_financial_status":      "SUCCEEDED",
					"contradictory_provider_status": "FAILED",
					"action":                        "PRESERVED_TERMINAL_SUCCESS_LOGGED_INCIDENT",
				},
			})
			record.ProcessingStatus = "PROCESSED"
			return op, nil

		case paypal.EventPayoutsItemReturned, paypal.EventPayoutsItemRefunded, paypal.EventPayoutsItemHeld:
			// Post-settlement return/refund/hold: initial payment succeeded and was settled.
			// Preserve SUCCEEDED financial state to protect claim completion integrity;
			// update provider status snapshot and log reversal incident.
			slog.WarnContext(ctx, "Provider reported post-settlement reversal for SUCCEEDED payout operation",
				slog.String("operation_id", op.ID),
				slog.String("event_type", event.EventType),
			)
			item, _, err := event.ParsePayoutItem()
			if err == nil && item != nil && item.TransactionStatus != "" {
				op.PayPalItemStatus = &item.TransactionStatus
				_ = s.repo.UpdatePayoutOperation(ctx, op)
			}

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
				EventType:      "PAYOUT_WEBHOOK_ITEM_REVERSAL_POST_SETTLEMENT",
				Metadata: map[string]interface{}{
					"operation_id":             op.ID,
					"paypal_event_id":          event.ID,
					"event_type":               event.EventType,
					"current_financial_status": "SUCCEEDED",
					"action":                   "RECORDED_POST_SETTLEMENT_REVERSAL_INCIDENT",
				},
			})
			record.ProcessingStatus = "PROCESSED"
			return op, nil
		}
	}

	if op.Status == "CANCELLED" && event.EventType == paypal.EventPayoutsItemSucceeded {
		// Payout was marked cancelled locally, but provider reports item SUCCEEDED!
		// Authoritative provider evidence proves funds were disbursed.
		// Transition operation to SUCCEEDED and case to COMPLETED to truthfully reflect financial
		// state and eliminate double-payout submission risks.
		slog.ErrorContext(ctx, "CRITICAL: Payout cancelled locally but PayPal reports item SUCCEEDED; transitioning to SUCCEEDED to prevent double payout",
			slog.String("operation_id", op.ID),
			slog.String("event_type", event.EventType),
		)
		item, _, err := event.ParsePayoutItem()
		if err == nil && item != nil {
			if op.PayPalPayoutItemID == nil && item.PayoutItemID != "" {
				op.PayPalPayoutItemID = &item.PayoutItemID
			}
		}
		itemSuccessStatus := "SUCCESS"
		op.PayPalItemStatus = &itemSuccessStatus
		op.Status = "SUCCEEDED"
		now := time.Now().UTC()
		op.CompletedAt = &now
		op.ErrorMessage = nil

		_ = s.repo.UpdatePayoutCaseStatus(ctx, op.OrganizationID, op.PayoutCaseID, "COMPLETED")
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
			EventType:      "PAYOUT_WEBHOOK_CANCELLED_OPERATION_EXECUTED_BY_PROVIDER",
			Metadata: map[string]interface{}{
				"operation_id":    op.ID,
				"paypal_event_id": event.ID,
				"event_type":      event.EventType,
				"action":          "TRANSITIONED_TO_SUCCEEDED_PREVENTED_DOUBLE_PAYOUT",
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

// RecoverPendingWebhookEvents processes any events previously stored in RECEIVED, FAILED (past backoff),
// or interrupted PROCESSING status using atomic database-level leasing.
func (s *Service) RecoverPendingWebhookEvents(ctx context.Context, olderThan time.Duration, maxEvents int) (int, error) {
	events, err := s.repo.ListPendingWebhookEvents(ctx, olderThan, maxEvents)
	if err != nil {
		return 0, fmt.Errorf("failed listing pending webhook events: %w", err)
	}

	recoveredCount := 0
	for i := range events {
		rec := &events[i]
		claimToken := crypto.GenerateUUID()
		leaseExpires := time.Now().UTC().Add(30 * time.Second)

		// Atomically acquire exclusive processing lease
		claimed, claimErr := s.repo.ClaimWebhookEventForProcessing(ctx, rec.ID, claimToken, leaseExpires)
		if claimErr != nil {
			// Another worker or concurrent redelivery already claimed or processed it; skip
			continue
		}

		rawBytes, mErr := json.Marshal(claimed.PayloadSnapshot)
		if mErr != nil {
			continue
		}
		var evt paypal.WebhookEvent
		if uErr := json.Unmarshal(rawBytes, &evt); uErr != nil {
			continue
		}

		op, rErr := s.reconcileWebhookEvent(ctx, &evt, claimed)
		if rErr != nil {
			errMsg := rErr.Error()
			attempts := claimed.ProcessingAttempts
			backoff := time.Duration(1<<attempts) * 30 * time.Second
			nextRetry := time.Now().UTC().Add(backoff)
			finalStatus := "FAILED"
			if attempts >= 5 {
				finalStatus = "DEAD_LETTER"
			}
			_ = s.repo.FinalizeWebhookEventProcessing(
				ctx,
				claimed.ID,
				claimToken,
				finalStatus,
				&errMsg,
				&nextRetry,
				claimed.OrganizationID,
				claimed.PayoutOperationID,
			)
			continue
		}

		opID := claimed.PayoutOperationID
		orgID := claimed.OrganizationID
		if op != nil {
			opID = &op.ID
			orgID = &op.OrganizationID
		}
		_ = s.repo.FinalizeWebhookEventProcessing(
			ctx,
			claimed.ID,
			claimToken,
			"PROCESSED",
			nil,
			nil,
			orgID,
			opID,
		)
		recoveredCount++
	}

	return recoveredCount, nil
}

// WebhookRecoveryWorker manages the background periodic loop to recover interrupted or pending webhook events.
type WebhookRecoveryWorker struct {
	service    *Service
	interval   time.Duration
	olderThan  time.Duration
	batchLimit int
	stopCh     chan struct{}
	doneCh     chan struct{}
	startOnce  sync.Once
	stopOnce   sync.Once
	running    atomic.Bool
}

// NewWebhookRecoveryWorker constructs a recovery worker with bounded batch limits and backoff intervals.
func (s *Service) NewWebhookRecoveryWorker(interval, olderThan time.Duration, batchLimit int) *WebhookRecoveryWorker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	if olderThan <= 0 {
		olderThan = 1 * time.Minute
	}
	if batchLimit <= 0 || batchLimit > 100 {
		batchLimit = 50
	}
	return &WebhookRecoveryWorker{
		service:    s,
		interval:   interval,
		olderThan:  olderThan,
		batchLimit: batchLimit,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}
}

// Start begins background periodic recovery. Protected by sync.Once to run exactly once per API process.
func (w *WebhookRecoveryWorker) Start(ctx context.Context) error {
	started := false
	w.startOnce.Do(func() {
		started = true
		w.running.Store(true)
		go w.run(ctx)
	})
	if !started {
		return errors.New("webhook recovery worker already started")
	}
	return nil
}

// Stop signals the recovery worker to terminate and waits gracefully up to the shutdown context deadline.
func (w *WebhookRecoveryWorker) Stop(shutdownCtx context.Context) error {
	var err error
	w.stopOnce.Do(func() {
		if !w.running.Load() {
			return
		}
		close(w.stopCh)
		select {
		case <-w.doneCh:
			w.running.Store(false)
		case <-shutdownCtx.Done():
			err = shutdownCtx.Err()
		}
	})
	return err
}

// IsRunning reports whether the worker is actively executing.
func (w *WebhookRecoveryWorker) IsRunning() bool {
	return w.running.Load()
}

func (w *WebhookRecoveryWorker) run(ctx context.Context) {
	defer close(w.doneCh)
	slog.Info("PayPal webhook background recovery worker started",
		slog.Duration("interval", w.interval),
		slog.Duration("older_than", w.olderThan),
		slog.Int("batch_limit", w.batchLimit),
	)

	// Execute initial sweep to catch any events pending from prior process restart
	w.executeRecoverySweep(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("PayPal webhook background recovery worker stopping (context cancelled)")
			return
		case <-w.stopCh:
			slog.Info("PayPal webhook background recovery worker stopping (graceful shutdown requested)")
			return
		case <-ticker.C:
			w.executeRecoverySweep(ctx)
		}
	}
}

func (w *WebhookRecoveryWorker) executeRecoverySweep(ctx context.Context) {
	sweepCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	recovered, err := w.service.RecoverPendingWebhookEvents(sweepCtx, w.olderThan, w.batchLimit)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Warn("PayPal webhook background recovery iteration failed",
				slog.String("error", err.Error()),
			)
		}
		return
	}
	if recovered > 0 {
		slog.Info("PayPal webhook background recovery sweep completed",
			slog.Int("recovered_count", recovered),
		)
	}
}

// StartWebhookRecoveryWorker initializes and starts the background worker.
func (s *Service) StartWebhookRecoveryWorker(ctx context.Context, interval, olderThan time.Duration, batchLimit int) *WebhookRecoveryWorker {
	worker := s.NewWebhookRecoveryWorker(interval, olderThan, batchLimit)
	_ = worker.Start(ctx)
	return worker
}
