package investigations

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"claimbridge/services/api/internal/auth"
	"claimbridge/services/api/internal/config"
	"claimbridge/services/api/internal/db"
)

// Handler handles HTTP requests for AI investigations.
type Handler struct {
	cfg     *config.Config
	service *Service
}

// NewHandler constructs a new investigations HTTP handler.
func NewHandler(cfg *config.Config, service *Service) *Handler {
	return &Handler{
		cfg:     cfg,
		service: service,
	}
}

// RegisterRoutes registers investigation routes to the ServeMux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/ai/status", h.handleAIStatus)
	mux.HandleFunc("POST /api/v1/payout-cases/{case_id}/investigations", h.handleCreateInvestigation)
	mux.HandleFunc("GET /api/v1/payout-cases/{case_id}/investigations", h.handleListInvestigations)
	mux.HandleFunc("GET /api/v1/investigations/{investigation_id}", h.handleGetInvestigation)
}

func (h *Handler) getAuth(r *http.Request) (*db.User, *db.Organization, error) {
	user, okUser := r.Context().Value(auth.UserContextKey).(*db.User)
	if !okUser || user == nil {
		return nil, nil, errors.New("unauthorized: missing or invalid session")
	}

	org, okOrg := r.Context().Value(auth.OrganizationContextKey).(*db.Organization)
	if !okOrg || org == nil {
		return nil, nil, errors.New("unauthorized: no active organization")
	}

	return user, org, nil
}

func (h *Handler) handleCreateInvestigation(w http.ResponseWriter, r *http.Request) {
	user, org, err := h.getAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	caseID := r.PathValue("case_id")
	if caseID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing case_id"})
		return
	}

	var req InvestigateRequest
	if r.Body != nil && r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	inv, err := h.service.InvestigatePayoutCase(r.Context(), org.ID, caseID, user.ID, req)
	if err != nil {
		if errors.Is(err, ErrCaseNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "payout case not found"})
			return
		}
		if errors.Is(err, ErrAIUnavailable) {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"error": "Investigation assistant is currently unavailable. Core payout operations remain fully functional.",
				"code":  "AI_UNAVAILABLE",
			})
			return
		}
		if errors.Is(err, ErrInvalidEvidenceReference) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
				"error": "Investigation failed: model output referenced invalid or ungrounded evidence identifiers.",
				"code":  "INVALID_EVIDENCE_REFERENCE",
			})
			return
		}
		slog.ErrorContext(r.Context(), "Investigation handler failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "internal investigation error",
		})
		return
	}

	writeJSON(w, http.StatusCreated, inv)
}

func (h *Handler) handleListInvestigations(w http.ResponseWriter, r *http.Request) {
	_, org, err := h.getAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	caseID := r.PathValue("case_id")
	if caseID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing case_id"})
		return
	}

	list, err := h.service.ListInvestigations(r.Context(), org.ID, caseID)
	if err != nil {
		if errors.Is(err, ErrCaseNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "payout case not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list investigations"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"investigations": list,
		"count":          len(list),
	})
}

func (h *Handler) handleGetInvestigation(w http.ResponseWriter, r *http.Request) {
	_, org, err := h.getAuth(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	invID := r.PathValue("investigation_id")
	if invID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing investigation_id"})
		return
	}

	inv, err := h.service.GetInvestigation(r.Context(), org.ID, invID)
	if err != nil {
		if errors.Is(err, ErrInvestigationNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "investigation not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to get investigation"})
		return
	}

	writeJSON(w, http.StatusOK, inv)
}

func (h *Handler) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	health := h.service.GetAIHealth(r.Context())
	statusStr := "ready"
	statusCode := http.StatusOK

	if !health.Reachable {
		statusStr = "unreachable"
		statusCode = http.StatusServiceUnavailable
	} else if !health.ModelAvailable {
		statusStr = "model_missing"
		statusCode = http.StatusServiceUnavailable
	}

	providerName := "none"
	modelName := "none"
	if h.service.aiProvider != nil {
		providerName = h.service.aiProvider.Name()
		modelName = h.service.aiProvider.Model()
	}

	writeJSON(w, statusCode, map[string]interface{}{
		"status":   statusStr,
		"provider": providerName,
		"model":    modelName,
		"health":   health,
	})
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
