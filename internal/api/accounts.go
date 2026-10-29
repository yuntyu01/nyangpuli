package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/yuntyu01/nyangpuli-lab/internal/persistence"
)

type AccountService interface {
	CreateGuest(context.Context) (string, error)
	Register(context.Context, string, string) (string, error)
	Login(context.Context, string, string) (string, error)
	LinkPassword(context.Context, string, string, string) error
	RevokeSession(context.Context, string) error
}

func (s *Server) publicAccountRoutes(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/v1/auth/guest" && r.URL.Path != "/v1/auth/register" && r.URL.Path != "/v1/auth/login" {
		return false
	}
	if r.Method != http.MethodPost {
		fail(w, 405, "method not allowed")
		return true
	}
	if s.Accounts == nil {
		fail(w, 503, "account service is not configured")
		return true
	}
	var token string
	var err error
	status := http.StatusCreated
	if r.URL.Path == "/v1/auth/guest" {
		token, err = s.Accounts.CreateGuest(r.Context())
	} else {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if !read(w, r, &req) {
			return true
		}
		if r.URL.Path == "/v1/auth/register" {
			token, err = s.Accounts.Register(r.Context(), req.Username, req.Password)
		} else {
			status = http.StatusOK
			token, err = s.Accounts.Login(r.Context(), req.Username, req.Password)
		}
	}
	if err != nil {
		switch {
		case errors.Is(err, persistence.ErrInvalidCredentialInput):
			fail(w, 400, "invalid username or password format")
		case errors.Is(err, persistence.ErrCredentialConflict):
			fail(w, 409, "credentials unavailable")
		case errors.Is(err, persistence.ErrInvalidCredentials):
			fail(w, 401, "invalid credentials or temporarily unavailable")
		default:
			fail(w, 500, "account operation failed")
		}
		return true
	}
	write(w, status, map[string]string{"token": token})
	return true
}
func (s *Server) authenticatedAccountRoutes(w http.ResponseWriter, r *http.Request, user, token string) bool {
	if r.URL.Path == "/v1/auth/me" {
		if r.Method != http.MethodGet {
			fail(w, http.StatusMethodNotAllowed, "method not allowed")
		} else {
			// The ID is resolved exclusively from the authenticated session or the
			// local server's token mapping, never from request headers or body.
			write(w, http.StatusOK, map[string]string{"account_id": user})
		}
		return true
	}
	if r.URL.Path != "/v1/auth/link" && r.URL.Path != "/v1/auth/logout" {
		return false
	}
	if r.Method != http.MethodPost {
		fail(w, 405, "method not allowed")
		return true
	}
	if s.Accounts == nil {
		fail(w, 503, "account service is not configured")
		return true
	}
	if r.URL.Path == "/v1/auth/logout" {
		if s.Accounts.RevokeSession(r.Context(), token) != nil {
			fail(w, 500, "logout failed")
		} else {
			write(w, 200, map[string]string{"status": "logged_out"})
		}
		return true
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !read(w, r, &req) {
		return true
	}
	err := s.Accounts.LinkPassword(r.Context(), user, req.Username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, persistence.ErrInvalidCredentialInput):
			fail(w, 400, "invalid username or password format")
		case errors.Is(err, persistence.ErrCredentialConflict):
			fail(w, 409, "credentials unavailable")
		default:
			fail(w, 500, "account operation failed")
		}
		return true
	}
	write(w, 200, map[string]string{"status": "linked"})
	return true
}
