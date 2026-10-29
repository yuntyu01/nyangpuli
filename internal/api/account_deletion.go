package api

import (
	"context"
	"net/http"
)

// AccountDeleter removes the PostgreSQL account and its private records.
type AccountDeleter interface {
	DeleteAccount(context.Context, string) error
}

func (s *Server) handlePostgresDataDeletion(w http.ResponseWriter, r *http.Request, user string) bool {
	path := r.URL.Path
	if path != "/v1/account" && path != "/v1/dev/account/postgres-data" {
		return false
	}
	if r.Method != http.MethodDelete {
		fail(w, http.StatusMethodNotAllowed, "method not allowed")
		return true
	}
	if s.Auth == nil || s.AccountDeletion == nil {
		fail(w, http.StatusServiceUnavailable, "account deletion requires PostgreSQL authentication and storage")
		return true
	}
	// An enabled remote memory service can hold records outside the transaction.
	// Refuse the complete route until that separate store has a coordinated flow.
	if path == "/v1/account" && (!s.PostgresOnlyStorage || s.Mem0 != nil) {
		fail(w, http.StatusServiceUnavailable, "account deletion requires PostgreSQL-only storage and no remote memory")
		return true
	}
	if err := s.AccountDeletion.DeleteAccount(r.Context(), user); err != nil {
		fail(w, http.StatusInternalServerError, "account deletion failed")
		return true
	}
	if path == "/v1/dev/account/postgres-data" {
		write(w, http.StatusOK, map[string]any{"status": "postgres_data_deleted_only", "complete_account_deletion": false, "remaining": "JSON reading files and external stores or attachments are not deleted by this endpoint"})
		return true
	}
	write(w, http.StatusOK, map[string]any{"status": "account_deleted", "scope": "postgresql_account_and_current_server_storage", "remaining": "external provider copies and backups require separate retention and deletion handling"})
	return true
}
