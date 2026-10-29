package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccountIdentityIsDerivedOnlyFromAuthenticatedSession(t *testing.T) {
	h := (&Server{Tokens: map[string]string{"token-a": "account-a", "token-b": "account-b"}}).Handler()
	call := func(method, token, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/v1/auth/me", strings.NewReader(body))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r.Header.Set("X-User-Id", "account-other")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct{ token, want string }{{"token-a", "account-a"}, {"token-b", "account-b"}} {
		w := call(http.MethodGet, tc.token, `{"user_id":"account-other"}`)
		if w.Code != 200 {
			t.Fatalf("status %d", w.Code)
		}
		var got map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got["account_id"] != tc.want {
			t.Fatalf("unexpected identity response: %#v", got)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("identity response must not be cached")
		}
		if strings.Contains(w.Body.String(), tc.token) {
			t.Fatal("response contains the token")
		}
	}
	for _, token := range []string{"", "invalid"} {
		if w := call(http.MethodGet, token, ""); w.Code != 401 {
			t.Fatalf("unauthorized token: %d", w.Code)
		}
	}
	if w := call(http.MethodPost, "token-a", ""); w.Code != 405 {
		t.Fatalf("wrong method: %d", w.Code)
	}
}
