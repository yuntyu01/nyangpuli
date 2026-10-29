package api

import (
	"context"
	"net/http"
)

// AccountDeleter deletes PostgreSQL account-scoped data. It is not a complete
// account-deletion operation while other stores and attachments exist.
type AccountDeleter interface {
	DeleteAccount(context.Context, string) error
}

// This deliberately named development route must never claim that all account
// data has been deleted. It is unavailable without database authentication.
func (s *Server) handlePostgresDataDeletion(w http.ResponseWriter, r *http.Request, user string) bool {
	if r.URL.Path != "/v1/dev/account/postgres-data" {
		return false
	}
	if r.Method != http.MethodDelete {
		fail(w, http.StatusMethodNotAllowed, "method not allowed")
		return true
	}
	if s.Auth == nil || s.AccountDeletion == nil {
		fail(w, http.StatusServiceUnavailable, "PostgreSQL account deletion is not configured")
		return true
	}
	if err := s.AccountDeletion.DeleteAccount(r.Context(), user); err != nil {
		fail(w, http.StatusInternalServerError, "PostgreSQL account data deletion failed")
		return true
	}
	write(w, http.StatusOK, map[string]any{
		"status":                    "postgres_data_deleted_only",
		"complete_account_deletion": false,
		"remaining":                 "JSON reading files and external stores or attachments are not deleted by this endpoint",
	})
	return true
}
