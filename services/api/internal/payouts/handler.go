package payouts

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"claimbridge/services/api/internal/auth"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
	"claimbridge/services/api/internal/paypal"
)

// Handler handles HTTP requests for Payout Cases, Operations, and PayPal integrations.
type Handler struct {
	cfg     *config.Config
	service *Service
}

// NewHandler constructs a new Payouts HTTP handler.
func NewHandler(cfg *config.Config, service *Service) *Handler {
	return &Handler{
		cfg:     cfg,
		service: service,
	}
}

// RegisterRoutes registers all payout and PayPal routes to the ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	// Connection & Status
	mux.HandleFunc("GET /api/v1/paypal/connection-status", h.handleConnectionStatus)

	// Webhooks (Unauthenticated at session layer; cryptographically verified)
	mux.HandleFunc("POST /api/v1/paypal/webhooks", h.handlePayPalWebhook)

	// Payout Cases
	mux.HandleFunc("GET /api/v1/payout-cases", h.handleListPayoutCases)
	mux.HandleFunc("POST /api/v1/payout-cases", h.handleCreatePayoutCase)
	mux.HandleFunc("GET /api/v1/payout-cases/{case_id}", h.handleGetPayoutCase)
	mux.HandleFunc("POST /api/v1/payout-cases/{case_id}/sync", h.handleSyncPayoutCase)

	// Payout Operations on Case
	mux.HandleFunc("POST /api/v1/payout-cases/{case_id}/payout-operations", h.handleInitiatePayoutOperation)
	mux.HandleFunc("GET /api/v1/payout-cases/{case_id}/operations", h.handleListPayoutOperations)

	// Evidence Reference Resolution
	mux.HandleFunc("GET /api/v1/payout-cases/{case_id}/evidence/{evidence_id}", h.handleResolveEvidence)

	// Payout Operations Actions
	mux.HandleFunc("POST /api/v1/payout-operations/{operation_id}/approve", h.handleApprovePayoutOperation)
	mux.HandleFunc("POST /api/v1/payout-operations/{operation_id}/submit", h.handleSubmitPayoutOperation)
	mux.HandleFunc("POST /api/v1/payout-operations/{operation_id}/cancel", h.handleCancelPayoutOperation)
	mux.HandleFunc("POST /api/v1/payout-operations/{operation_id}/recover", h.handleRecoverPayoutOperation)

	// Convenience Aliases
	mux.HandleFunc("POST /api/v1/payout-cases/{case_id}/operations/{operation_id}/submit", h.handleSubmitPayoutOperation)
	mux.HandleFunc("POST /api/v1/payout-cases/{case_id}/operations/{operation_id}/cancel", h.handleCancelPayoutOperation)
	mux.HandleFunc("POST /api/v1/payout-cases/{case_id}/operations/{operation_id}/recover", h.handleRecoverPayoutOperation)
}

func (h *Handler) getAuth(r *http.Request) (*db.User, *db.Organization, error) {
	user, okUser := r.Context().Value(auth.UserContextKey).(*db.User)
	if !okUser || user == nil {
		return nil, nil, errors.New("unauthorized: missing or invalid session")
	}

	org, okOrg := r.Context().Value(auth.OrganizationContextKey).(*db.Organization)
	if !okOrg || org == nil {
		return nil, nil, errors.New("forbidden: active organization not found in session")
	}

	return user, org, nil
}

// GET /api/v1/paypal/connection-status
func (h *Handler) handleConnectionStatus(w http.ResponseWriter, r *http.Request) {
	user, _, err := h.getAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	_ = user
	status := h.service.CheckConnection(r.Context())
	code := http.StatusOK
	if !status.Connected {
		code = http.StatusServiceUnavailable
	}

	writeJSON(w, code, status)
}

// GET /api/v1/payout-cases
func (h *Handler) handleListPayoutCases(w http.ResponseWriter, r *http.Request) {
	_, org, err := h.getAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	limit := 20
	offset := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			if parsed > 100 {
				parsed = 100
			}
			limit = parsed
		}
	}
	if o := r.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	cases, total, err := h.service.ListPayoutCases(r.Context(), org.ID, limit, offset)
	if err != nil {
		slog.ErrorContext(r.Context(), "Failed to list payout cases", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list payout cases"})
		return
	}

	if cases == nil {
		cases = []db.PayoutCase{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cases":  cases,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// POST /api/v1/payout-cases
func (h *Handler) handleCreatePayoutCase(w http.ResponseWriter, r *http.Request) {
	user, org, err := h.getAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 128*1024)
	var req CreateCaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	pCase, err := h.service.CreatePayoutCase(r.Context(), user, org, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidAmount), errors.Is(err, ErrAmountExceedsCeiling),
			errors.Is(err, ErrUnsupportedCurrency), errors.Is(err, ErrInvalidRecipientEmail),
			errors.Is(err, ErrInvalidRecipientName), errors.Is(err, ErrInvalidTitle):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			slog.ErrorContext(r.Context(), "Failed to create payout case", slog.String("error", err.Error()))
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create payout case"})
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"case":    pCase,
		"message": "Payout case created successfully",
	})
}

// GET /api/v1/payout-cases/{case_id}
func (h *Handler) handleGetPayoutCase(w http.ResponseWriter, r *http.Request) {
	_, org, err := h.getAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	caseID := r.PathValue("case_id")
	if caseID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "case_id is required"})
		return
	}

	pCase, ops, err := h.service.GetPayoutCase(r.Context(), org.ID, caseID)
	if err != nil {
		if errors.Is(err, db.ErrPayoutCaseNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "payout case not found"})
			return
		}
		slog.ErrorContext(r.Context(), "Failed to retrieve payout case", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to retrieve payout case"})
		return
	}

	if ops == nil {
		ops = []db.PayoutOperation{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"case":       pCase,
		"operations": ops,
	})
}

// POST /api/v1/payout-cases/{case_id}/sync
func (h *Handler) handleSyncPayoutCase(w http.ResponseWriter, r *http.Request) {
	_, org, err := h.getAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	caseID := r.PathValue("case_id")
	if caseID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "case_id is required"})
		return
	}

	pCase, op, err := h.service.SyncPayoutCase(r.Context(), org.ID, caseID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrPayoutCaseNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "payout case not found"})
		case errors.Is(err, ErrFixtureExternalOperationForbidden):
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": err.Error(),
				"code":  "FIXTURE_EXTERNAL_OPERATION_FORBIDDEN",
			})
		case errors.Is(err, ErrNoPayPalBatchID):
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":     err.Error(),
				"code":      "NO_PAYPAL_BATCH_ID",
				"case":      pCase,
				"operation": op,
			})
		case errors.Is(err, ErrNoOperationsToSync):
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		default:
			slog.ErrorContext(r.Context(), "Sync failed", slog.String("error", err.Error()))
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"case":      pCase,
		"operation": op,
		"message":   "Successfully synchronized with PayPal Sandbox",
	})
}

// POST /api/v1/payout-cases/{case_id}/payout-operations
func (h *Handler) handleInitiatePayoutOperation(w http.ResponseWriter, r *http.Request) {
	user, org, err := h.getAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	caseID := r.PathValue("case_id")
	if caseID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "case_id is required"})
		return
	}

	op, err := h.service.InitiatePayoutOperation(r.Context(), user, org, caseID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrPayoutCaseNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "payout case not found"})
		case errors.Is(err, ErrActiveOperationExists), errors.Is(err, ErrCaseInTerminalState):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			slog.ErrorContext(r.Context(), "Initiate payout operation failed", slog.String("error", err.Error()))
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"operation": op,
		"message":   "Payout operation initiated successfully",
	})
}

// GET /api/v1/payout-cases/{case_id}/operations
func (h *Handler) handleListPayoutOperations(w http.ResponseWriter, r *http.Request) {
	_, org, err := h.getAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	caseID := r.PathValue("case_id")
	ops, err := h.service.repo.ListPayoutOperationsByCaseID(r.Context(), org.ID, caseID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list operations"})
		return
	}

	if ops == nil {
		ops = []db.PayoutOperation{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"operations": ops,
	})
}

// POST /api/v1/payout-operations/{operation_id}/approve
func (h *Handler) handleApprovePayoutOperation(w http.ResponseWriter, r *http.Request) {
	user, org, err := h.getAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	opID := r.PathValue("operation_id")
	if opID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "operation_id is required"})
		return
	}

	op, err := h.service.ApprovePayoutOperation(r.Context(), user, org, opID)
	if err != nil {
		switch {
		case errors.Is(err, ErrSelfApprovalForbidden):
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "Self-approval is forbidden. Dual-custody policy mandates that a distinct authorized officer must approve this payout.",
			})
		case errors.Is(err, db.ErrPayoutOperationNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "payout operation not found"})
		case errors.Is(err, ErrCaseInTerminalState), errors.Is(err, db.ErrInvalidOperationTransition):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"operation": op,
		"message":   "Payout operation approved successfully",
	})
}

// POST /api/v1/payout-operations/{operation_id}/submit
func (h *Handler) handleSubmitPayoutOperation(w http.ResponseWriter, r *http.Request) {
	user, org, err := h.getAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	opID := r.PathValue("operation_id")
	if opID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "operation_id is required"})
		return
	}

	// Fetch op to get its case_id
	existingOp, err := h.service.repo.GetPayoutOperationByID(r.Context(), org.ID, opID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "payout operation not found"})
		return
	}

	op, err := h.service.SubmitPayoutOperation(r.Context(), user, org, existingOp.PayoutCaseID, opID)
	if err != nil {
	switch {
		case errors.Is(err, ErrFixtureExternalOperationForbidden):
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": err.Error(),
				"code":  "FIXTURE_EXTERNAL_OPERATION_FORBIDDEN",
			})
		case errors.Is(err, ErrOperationNotApproved):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrOperationAlreadySubmitted), errors.Is(err, ErrOperationCannotBeSubmitted), errors.Is(err, ErrCaseInTerminalState), errors.Is(err, db.ErrSubmissionClaimConflict):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		case errors.Is(err, paypal.ErrMissingCredentials), errors.Is(err, ErrPayPalNotConfigured):
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
				"error":     "PayPal credentials not configured",
				"code":      "PAYPAL_NOT_CONFIGURED",
				"operation": op,
			})
		case errors.Is(err, paypal.ErrUnauthorized):
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{
				"error":     "PayPal OAuth authentication failed: invalid credentials",
				"code":      "PAYPAL_OAUTH_FAILED",
				"operation": op,
			})
		case errors.Is(err, paypal.ErrMissingPayoutsScope):
			writeJSON(w, http.StatusForbidden, map[string]interface{}{
				"error":     "PayPal OAuth token missing required scope https://uri.paypal.com/payments/payouts",
				"code":      "PAYPAL_SCOPE_MISSING",
				"operation": op,
			})
		case errors.Is(err, ErrPayPalAccountRestricted), errors.Is(err, paypal.ErrAccountRestricted):
			writeJSON(w, http.StatusForbidden, map[string]interface{}{
				"error":     err.Error(),
				"code":      "PAYPAL_ACCOUNT_RESTRICTED",
				"operation": op,
			})
		case errors.Is(err, paypal.ErrForbidden):
			writeJSON(w, http.StatusForbidden, map[string]interface{}{
				"error":     err.Error(),
				"code":      "PAYPAL_FORBIDDEN",
				"operation": op,
			})
		case paypal.IsAmbiguousOutcome(err) || errors.Is(err, paypal.ErrAmbiguousNetworkTimeout):
			writeJSON(w, http.StatusGatewayTimeout, map[string]interface{}{
				"error":     "Payout submission ambiguous. Status transitioned to RECONCILIATION_REQUIRED for safety.",
				"operation": op,
			})
		default:
			slog.ErrorContext(r.Context(), "Payout submission failed", slog.String("error", err.Error()))
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{
				"error":     err.Error(),
				"operation": op,
			})
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"operation": op,
		"message":   "Payout successfully submitted to PayPal Sandbox",
	})
}

// POST /api/v1/payout-operations/{operation_id}/cancel
func (h *Handler) handleCancelPayoutOperation(w http.ResponseWriter, r *http.Request) {
	user, org, err := h.getAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	opID := r.PathValue("operation_id")
	if opID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "operation_id is required"})
		return
	}

	op, err := h.service.CancelPayoutOperation(r.Context(), user, org, opID)
	if err != nil {
		switch {
		case errors.Is(err, ErrFixtureExternalOperationForbidden):
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": err.Error(),
				"code":  "FIXTURE_EXTERNAL_OPERATION_FORBIDDEN",
			})
		case errors.Is(err, db.ErrPayoutOperationNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "payout operation not found"})
		case errors.Is(err, ErrOperationAlreadyCancelled), errors.Is(err, ErrNotEligibleForCancellation):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		default:
			slog.ErrorContext(r.Context(), "Cancellation failed", slog.String("error", err.Error()))
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"operation": op,
		"message":   "Payout operation successfully cancelled",
	})
}

// POST /api/v1/payout-operations/{operation_id}/recover
func (h *Handler) handleRecoverPayoutOperation(w http.ResponseWriter, r *http.Request) {
	user, org, err := h.getAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	opID := r.PathValue("operation_id")
	if opID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "operation_id is required"})
		return
	}

	pCase, op, outcome, err := h.service.RecoverPayoutOperation(r.Context(), user, org, opID)
	if err != nil {
		switch {
		case errors.Is(err, db.ErrPayoutOperationNotFound), errors.Is(err, db.ErrPayoutCaseNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error(), "code": "NOT_FOUND"})
		case errors.Is(err, ErrFixtureExternalOperationForbidden):
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": err.Error(),
				"code":  "FIXTURE_EXTERNAL_OPERATION_FORBIDDEN",
			})
		case errors.Is(err, ErrOperationNotApproved):
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": err.Error(),
				"code":  "OPERATION_NOT_APPROVED",
			})
		case errors.Is(err, ErrRecoveryAlreadyInProgress):
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":   err.Error(),
				"code":    "RECOVERY_ALREADY_IN_PROGRESS",
				"outcome": outcome,
			})
		case errors.Is(err, ErrRecoveryClaimConflict):
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":   err.Error(),
				"code":    "RECOVERY_CLAIM_CONFLICT",
				"outcome": outcome,
			})
		case errors.Is(err, ErrStaleRecoveryClaim), errors.Is(err, ErrRecoveryLeaseExpired):
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":     err.Error(),
				"code":      "STALE_RECOVERY_CLAIM",
				"outcome":   outcome,
				"operation": op,
			})
		case errors.Is(err, ErrRecoveryWindowExpired):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
				"error":     err.Error(),
				"code":      "RECOVERY_WINDOW_EXPIRED",
				"outcome":   outcome,
				"operation": op,
			})
		case errors.Is(err, ErrSubmissionTimestampMissing), errors.Is(err, ErrSubmissionTimestampInvalid):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
				"error":     err.Error(),
				"code":      "INVALID_SUBMISSION_TIMESTAMP",
				"outcome":   outcome,
				"operation": op,
			})
		case errors.Is(err, ErrOperationNotEligibleForRecovery):
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":     err.Error(),
				"code":      "OPERATION_NOT_ELIGIBLE",
				"outcome":   outcome,
				"operation": op,
			})
		case errors.Is(err, ErrCaseInTerminalState):
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":   err.Error(),
				"code":    "CASE_IN_TERMINAL_STATE",
				"outcome": outcome,
			})
		case errors.Is(err, ErrFingerprintMismatch):
			writeJSON(w, http.StatusPreconditionFailed, map[string]interface{}{
				"error":     err.Error(),
				"code":      "FINGERPRINT_MISMATCH",
				"outcome":   outcome,
				"operation": op,
			})
		case errors.Is(err, ErrFingerprintMissing):
			writeJSON(w, http.StatusPreconditionFailed, map[string]interface{}{
				"error":     err.Error(),
				"code":      "FINGERPRINT_MISSING",
				"outcome":   outcome,
				"operation": op,
			})
		case errors.Is(err, ErrManualReconciliationRequired):
			writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
				"error":     err.Error(),
				"code":      "MANUAL_RECONCILIATION_REQUIRED",
				"outcome":   outcome,
				"operation": op,
			})
		default:
			slog.ErrorContext(r.Context(), "Payout recovery attempt unresolved",
				slog.String("operation_id", opID),
				slog.String("outcome", outcome),
				slog.String("error", err.Error()),
			)
			writeJSON(w, http.StatusBadGateway, map[string]interface{}{
				"error":     err.Error(),
				"code":      "RECOVERY_UNRESOLVED",
				"outcome":   outcome,
				"operation": op,
			})
		}
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"case":      pCase,
		"operation": op,
		"outcome":   outcome,
		"message":   fmt.Sprintf("Payout recovery completed with outcome: %s", outcome),
	})
}

// GET /api/v1/payout-cases/{case_id}/evidence/{evidence_id}
func (h *Handler) handleResolveEvidence(w http.ResponseWriter, r *http.Request) {
	_, org, err := h.getAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	caseID := r.PathValue("case_id")
	if caseID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "case_id is required",
			"code":  "INVALID_CASE_ID",
		})
		return
	}

	evidenceID := r.PathValue("evidence_id")
	if evidenceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "evidence_id is required",
			"code":  "INVALID_EVIDENCE_ID",
		})
		return
	}

	resolution, err := h.service.ResolveEvidence(r.Context(), org.ID, caseID, evidenceID)
	if err != nil {
		if errors.Is(err, ErrEvidenceNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]interface{}{
				"reference_id":      evidenceID,
				"resolution_status": "UNAVAILABLE",
				"error":             "evidence reference not found",
				"code":              "EVIDENCE_NOT_FOUND",
			})
			return
		}
		slog.ErrorContext(r.Context(), "Failed to resolve evidence reference",
			slog.String("case_id", caseID),
			slog.String("evidence_id", evidenceID),
			slog.String("error", err.Error()),
		)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "failed to resolve evidence reference",
			"code":  "INTERNAL_ERROR",
		})
		return
	}

	writeJSON(w, http.StatusOK, resolution)
}

func (h *Handler) handlePayPalWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	// 1. Content-Type validation
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(strings.ToLower(ct), "application/json") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Content-Type must be application/json"})
		return
	}

	// 2. Body limit check (512 KB maximum)
	r.Body = http.MaxBytesReader(w, r.Body, 512*1024)
	rawBody, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Failed reading request body or body exceeds 512KB limit"})
		return
	}

	// 3. Extract required PayPal webhook headers
	headers := paypal.WebhookHeaders{
		TransmissionID:   r.Header.Get("PAYPAL-TRANSMISSION-ID"),
		TransmissionTime: r.Header.Get("PAYPAL-TRANSMISSION-TIME"),
		CertURL:          r.Header.Get("PAYPAL-CERT-URL"),
		AuthAlgo:         r.Header.Get("PAYPAL-AUTH-ALGO"),
		TransmissionSig:  r.Header.Get("PAYPAL-TRANSMISSION-SIG"),
	}

	// 4. Validate header presence
	if strings.TrimSpace(headers.TransmissionID) == "" ||
		strings.TrimSpace(headers.TransmissionTime) == "" ||
		strings.TrimSpace(headers.CertURL) == "" ||
		strings.TrimSpace(headers.AuthAlgo) == "" ||
		strings.TrimSpace(headers.TransmissionSig) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "Missing required PayPal webhook signature headers",
		})
		return
	}

	// 5. Enforce webhook ID configuration
	if strings.TrimSpace(h.cfg.PayPalWebhookID) == "" {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error": "PayPal webhook integration is not configured on this server",
		})
		return
	}

	// 6. Process Webhook
	res, err := h.service.ProcessPayPalWebhook(r.Context(), headers, rawBody)
	if err != nil {
		if errors.Is(err, ErrWebhookUnconfigured) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
			return
		}
		if errors.Is(err, paypal.ErrInvalidWebhookSignature) ||
			errors.Is(err, paypal.ErrMissingWebhookHeaders) ||
			errors.Is(err, paypal.ErrInvalidAuthAlgo) ||
			errors.Is(err, paypal.ErrInvalidCertURL) ||
			errors.Is(err, paypal.ErrSSRFPrevented) ||
			errors.Is(err, paypal.ErrExpiredCert) {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error":   "Webhook cryptographic verification failed",
				"details": err.Error(),
			})
			return
		}
		if strings.Contains(err.Error(), "malformed") || strings.Contains(err.Error(), "missing required") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		slog.ErrorContext(r.Context(), "Webhook processing failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to process webhook event",
		})
		return
	}

	// 7. Output responses
	if res.IsDuplicate {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":          "duplicate_acknowledged",
			"paypal_event_id": res.Event.PayPalEventID,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":          "processed",
		"paypal_event_id": res.Event.PayPalEventID,
		"event_type":      res.Event.EventType,
	})
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
