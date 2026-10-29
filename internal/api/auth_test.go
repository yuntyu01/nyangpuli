package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeAuth struct{}

func (fakeAuth) Authenticate(_ context.Context, token string) (string, error) {
	if token == "valid" {
		return "account-a", nil
	}
	return "", errors.New("invalid")
}
func TestConfiguredAuthenticatorDoesNotFallBackToStaticTokens(t *testing.T) {
	s := Server{Auth: fakeAuth{}, Tokens: map[string]string{"static": "wrong"}}
	for _, tc := range []struct {
		token  string
		status int
	}{{"static", 401}, {"invalid", 401}, {"valid", 404}} {
		req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
		req.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, req)
		if w.Code != tc.status {
			t.Fatalf("%s: got %d want %d", tc.token, w.Code, tc.status)
		}
	}
}
