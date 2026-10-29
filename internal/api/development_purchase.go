package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/yuntyu01/nyangpuli-lab/internal/purchase"
)

type DevelopmentPurchaser interface {
	Simulate(context.Context, string, string, string, purchase.Status) error
	List(context.Context, string) ([]purchase.DevelopmentRecord, error)
}

func (s *Server) handleDevelopmentPurchase(w http.ResponseWriter, r *http.Request, user string) bool {
	if r.URL.Path != "/v1/dev/purchases" && r.URL.Path != "/v1/dev/purchases/restore" {
		return false
	}
	if !s.AllowTestUnlock || s.DevelopmentPurchaser == nil || s.Auth == nil {
		fail(w, http.StatusNotFound, "route not found")
		return true
	}
	restore := r.URL.Path == "/v1/dev/purchases/restore"
	if (!restore && r.Method == http.MethodGet) || (restore && r.Method == http.MethodPost) {
		purchases, err := s.DevelopmentPurchaser.List(r.Context(), user)
		if err != nil {
			fail(w, http.StatusInternalServerError, "simulation lookup failed")
			return true
		}
		write(w, http.StatusOK, map[string]any{"kind": "development_simulation_not_store_restore", "purchases": purchases})
		return true
	}
	if restore || r.Method != http.MethodPost {
		fail(w, http.StatusMethodNotAllowed, "method not allowed")
		return true
	}
	var request struct {
		ReadingID     string          `json:"reading_id"`
		TransactionID string          `json:"transaction_id"`
		Status        purchase.Status `json:"status"`
	}
	if !read(w, r, &request) {
		return true
	}
	if err := s.DevelopmentPurchaser.Simulate(r.Context(), user, request.ReadingID, request.TransactionID, request.Status); err != nil {
		switch {
		case errors.Is(err, purchase.ErrOwnership), errors.Is(err, purchase.ErrInvalid):
			fail(w, http.StatusConflict, "simulation rejected")
		default:
			fail(w, http.StatusInternalServerError, "simulation failed")
		}
		return true
	}
	// Always report persisted state: a stale success event after refund is
	// intentionally ignored and must not be reported as an active purchase.
	rows, err := s.DevelopmentPurchaser.List(r.Context(), user)
	if err != nil {
		fail(w, http.StatusInternalServerError, "simulation lookup failed")
		return true
	}
	for _, p := range rows {
		if p.TransactionID == request.TransactionID && p.ReadingID == request.ReadingID {
			write(w, http.StatusOK, map[string]string{"status": string(p.Status), "kind": "development_simulation_not_payment"})
			return true
		}
	}
	fail(w, http.StatusInternalServerError, "simulation state unavailable")
	return true
}
