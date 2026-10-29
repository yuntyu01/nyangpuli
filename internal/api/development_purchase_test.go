package api

import (
	"context"
	"strings"
	"testing"

	"github.com/yuntyu01/nyangpuli-lab/internal/purchase"
)

type devPurchaseSpy struct {
	user   string
	called bool
	status purchase.Status
}

func (d *devPurchaseSpy) List(_ context.Context, user string) ([]purchase.DevelopmentRecord, error) {
	d.user = user
	d.called = true
	status := d.status
	if status == "" {
		status = purchase.Purchased
	}
	return []purchase.DevelopmentRecord{{TransactionID: "t", ReadingID: "r", Status: status}}, nil
}

func (d *devPurchaseSpy) Simulate(_ context.Context, user, reading, transaction string, status purchase.Status) error {
	d.called = true
	d.user = user
	return nil
}
func TestDevelopmentPurchaseRequiresExplicitModeAndSession(t *testing.T) {
	p := &devPurchaseSpy{}
	path := "/v1/dev/purchases"
	data := `{"reading_id":"r","transaction_id":"t","status":"purchased"}`
	for _, server := range []*Server{{Auth: fakeAuth{}, DevelopmentPurchaser: p}, {Tokens: map[string]string{"valid": "a"}, AllowTestUnlock: true, DevelopmentPurchaser: p}} {
		w := call(server.Handler(), "POST", path, "valid", data)
		if w.Code != 404 || p.called {
			t.Fatal(w.Code, p.called)
		}
	}
	h := (&Server{Auth: fakeAuth{}, AllowTestUnlock: true, DevelopmentPurchaser: p}).Handler()
	if w := call(h, "POST", path, "", data); w.Code != 401 || p.called {
		t.Fatal(w.Code)
	}
	w := call(h, "POST", path, "valid", data)
	if w.Code != 200 || p.user != "account-a" || !strings.Contains(w.Body.String(), "development_simulation_not_payment") {
		t.Fatal(w.Code, w.Body.String(), p.user)
	}
	for _, tc := range []struct{ method, path string }{{"GET", path}, {"POST", path + "/restore"}} {
		w = call(h, tc.method, tc.path, "valid", "")
		if w.Code != 200 || p.user != "account-a" || !strings.Contains(w.Body.String(), "development_simulation_not_store_restore") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	p.status = purchase.Refunded
	w = call(h, "POST", path, "valid", data)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"refunded"`) {
		t.Fatal("stale event did not return saved state", w.Code, w.Body.String())
	}
}
