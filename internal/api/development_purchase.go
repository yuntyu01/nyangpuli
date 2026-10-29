package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/yuntyu01/nyangpuli-lab/internal/purchase"
)

type DevelopmentPurchaser interface {
	Simulate(context.Context, string, string, string, purchase.Status) error
}

func (s *Server) handleDevelopmentPurchase(w http.ResponseWriter, r *http.Request, user string) bool {
	if r.URL.Path != "/v1/dev/purchases" {
		return false
	}
	if !s.AllowTestUnlock || s.DevelopmentPurchaser == nil || s.Auth == nil {
		fail(w, http.StatusNotFound, "route not found")
		return true
	}
	if r.Method != http.MethodPost {
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
	write(w, http.StatusOK, map[string]string{"status": string(request.Status), "kind": "development_simulation_not_payment"})
	return true
}
