package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/yuntyu01/nyangpuli-lab/internal/memory"
)

type deletionSpy struct {
	user  string
	err   error
	calls int
}

func (d *deletionSpy) DeleteAccount(_ context.Context, user string) error {
	d.user = user
	d.calls++
	return d.err
}
func TestPostgresDeletionRequiresAuthenticationAndReportsPartialScope(t *testing.T) {
	d := &deletionSpy{}
	h := (&Server{Auth: fakeAuth{}, AccountDeletion: d}).Handler()
	path := "/v1/dev/account/postgres-data"
	if w := call(h, "DELETE", path, "", ""); w.Code != 401 || d.calls != 0 {
		t.Fatal(w.Code, d.calls)
	}
	if w := call(h, "POST", path, "valid", ""); w.Code != 405 || d.calls != 0 {
		t.Fatal(w.Code, d.calls)
	}
	w := call(h, "DELETE", path, "valid", `{"user_id":"victim"}`)
	if w.Code != 200 || d.user != "account-a" || d.calls != 1 {
		t.Fatal(w.Code, d.user, d.calls)
	}
	for _, text := range []string{`"status":"postgres_data_deleted_only"`, `"complete_account_deletion":false`, `JSON reading files`} {
		if !strings.Contains(w.Body.String(), text) {
			t.Fatal(w.Body.String())
		}
	}
	d.err = errors.New("sensitive database details")
	w = call(h, "DELETE", path, "valid", "")
	if w.Code != 500 || strings.Contains(w.Body.String(), "sensitive database details") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestPostgresDeletionUnavailableForStaticTokenMode(t *testing.T) {
	d := &deletionSpy{}
	h := (&Server{Tokens: map[string]string{"local": "alice"}, AccountDeletion: d}).Handler()
	w := call(h, "DELETE", "/v1/dev/account/postgres-data", "local", "")
	if w.Code != 503 || d.calls != 0 {
		t.Fatal(w.Code, d.calls)
	}
}

func TestAccountDeleteRequiresDatabaseAndNoRemoteMemory(t *testing.T) {
	d := &deletionSpy{}
	path := "/v1/account"
	h := (&Server{Auth: fakeAuth{}, AccountDeletion: d, PostgresOnlyStorage: true}).Handler()
	w := call(h, "DELETE", path, "valid", `{"user_id":"someone-else"}`)
	if w.Code != 200 || d.user != "account-a" || !strings.Contains(w.Body.String(), `"status":"account_deleted"`) {
		t.Fatal(w.Code, w.Body.String(), d.user)
	}
	h = (&Server{Tokens: map[string]string{"valid": "a"}, AccountDeletion: d}).Handler()
	if w = call(h, "DELETE", path, "valid", ""); w.Code != 503 || d.calls != 1 {
		t.Fatal(w.Code, d.calls)
	}
	remote, err := memory.NewMem0("http://127.0.0.1:9001", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	h = (&Server{Auth: fakeAuth{}, AccountDeletion: d, PostgresOnlyStorage: true, Mem0: remote}).Handler()
	if w = call(h, "DELETE", path, "valid", ""); w.Code != 503 || d.calls != 1 {
		t.Fatal(w.Code, d.calls)
	}
}
