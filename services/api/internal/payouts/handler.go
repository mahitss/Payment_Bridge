package payouts

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

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

	// Payout Cases
	mux.HandleFunc("GET /api/v1/payout-cases", h.handleListPayoutCases)
	mux.HandleFunc("POST /api/v1/payout-cases", h.handleCreatePayoutCase)
	mux.HandleFunc("GET /api/v1/payout-cases/{case_id}", h.handleGetPayoutCase)
	mux.HandleFunc("POST /api/v1/payout-cases/{case_id}/sync", h.handleSyncPayoutCase)

	// Payout Operations on Case
	mux.HandleFunc("POST /api/v1/payout-cases/{case_id}/payout-operations", h.handleInitiatePayoutOperation)
	mux.HandleFunc("GET /api/v1/payout-cases/{case_id}/operations", h.handleListPayoutOperations)

	// Payout Operations Actions
	mux.HandleFunc("POST /api/v1/payout-operations/{operation_id}/approve", h.handleApprovePayoutOperation)
	mux.HandleFunc("POST /api/v1/payout-operations/{operation_id}/submit", h.handleSubmitPayoutOperation)
	mux.HandleFunc("POST /api/v1/payout-operations/{operation_id}/cancel", h.handleCancelPayoutOperation)

	// Convenience Aliases
	mux.HandleFunc("POST /api/v1/payout-cases/{case_id}/operations/{operation_id}/submit", h.handleSubmitPayoutOperation)
	mux.HandleFunc("POST /api/v1/payout-cases/{case_id}/operations/{operation_id}/cancel", h.handleCancelPayoutOperation)
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
		if errors.Is(err, db.ErrPayoutCaseNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "payout case not found"})
			return
		}
		slog.ErrorContext(r.Context(), "Initiate payout operation failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
		case errors.Is(err, ErrOperationNotApproved):
			writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error()})
		case errors.Is(err, ErrOperationAlreadySubmitted):
			writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		case errors.Is(err, paypal.ErrAmbiguousNetworkTimeout):
			writeJSON(w, http.StatusGatewayTimeout, map[string]interface{}{
				"error":     "Payout submission timed out. Status transitioned to RECONCILIATION_REQUIRED for safety.",
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
		case errors.Is(err, db.ErrPayoutOperationNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "payout operation not found"})
		case errors.Is(err, ErrNotEligibleForCancellation):
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

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
