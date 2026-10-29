package purchase

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type lookupFunc func(context.Context, StoreReference) (StoreSnapshot, error)

func (f lookupFunc) Lookup(ctx context.Context, r StoreReference) (StoreSnapshot, error) {
	return f(ctx, r)
}

type bindingsFunc func(context.Context, string, string) (string, error)

func (f bindingsFunc) Resolve(ctx context.Context, s, k string) (string, error) { return f(ctx, s, k) }

func TestStoreAdapterUsesOnlyAuthenticatedAccountBinding(t *testing.T) {
	ctx := context.Background()
	lookup := lookupFunc(func(_ context.Context, r StoreReference) (StoreSnapshot, error) {
		if r.TransactionID != "12345" {
			t.Fatalf("wrong transaction reference")
		}
		return StoreSnapshot{TransactionID: "12345", ProductID: "detail", ApplicationID: "app.example", Environment: "sandbox", StoreAccountToken: "trusted-token", Status: Purchased, Revision: 5}, nil
	})
	bindings := bindingsFunc(func(_ context.Context, store, token string) (string, error) {
		if store != "apple" || token != "trusted-token" {
			return "", errors.New("bad binding")
		}
		return "server-user", nil
	})
	v, err := NewStoreAdapter("apple", "app.example", "sandbox", lookup, bindings)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Verify(ctx, []byte(`{"transaction_id":"12345"}`))
	if err != nil || got.AccountID != "server-user" || got.Status != Purchased || got.Revision != 5 {
		t.Fatalf("unexpected verified result: %#v %v", got, err)
	}
	// Supplying an account in client proof is rejected, even if plausible.
	if _, err := v.Verify(ctx, []byte(`{"transaction_id":"12345","account_id":"other"}`)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("client account was accepted: %v", err)
	}
}

func TestStoreAdapterRejectsMismatchedStoreState(t *testing.T) {
	valid := StoreSnapshot{TransactionID: "999", ProductID: "detail", ApplicationID: "app.example", Environment: "production", StoreAccountToken: "bound", Status: Purchased, Revision: 7}
	bindings := bindingsFunc(func(context.Context, string, string) (string, error) { return "user", nil })
	tests := []struct {
		name   string
		change func(*StoreSnapshot)
	}{
		{"application", func(s *StoreSnapshot) { s.ApplicationID = "other" }},
		{"environment", func(s *StoreSnapshot) { s.Environment = "sandbox" }},
		{"product", func(s *StoreSnapshot) { s.ProductID = "other" }},
		{"account missing", func(s *StoreSnapshot) { s.StoreAccountToken = "" }},
		{"revision missing", func(s *StoreSnapshot) { s.Revision = 0 }},
		{"status unknown", func(s *StoreSnapshot) { s.Status = "unknown" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := valid
			tc.change(&snapshot)
			v, err := NewStoreAdapter("google", "app.example", "production", lookupFunc(func(context.Context, StoreReference) (StoreSnapshot, error) { return snapshot, nil }), bindings)
			if err != nil {
				t.Fatal(err)
			}
			_, err = v.Verify(context.Background(), []byte(`{"product_id":"detail","purchase_token":"secret-token"}`))
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestStoreAdapterRejectsInvalidProofAndDoesNotExposeProviderErrors(t *testing.T) {
	calls := 0
	lookup := lookupFunc(func(context.Context, StoreReference) (StoreSnapshot, error) {
		calls++
		return StoreSnapshot{}, errors.New("provider failure secret-token")
	})
	bindings := bindingsFunc(func(context.Context, string, string) (string, error) { return "", errors.New("bad") })
	v, _ := NewStoreAdapter("google", "app.example", "production", lookup, bindings)
	for _, proof := range []string{`{}`, `{"product_id":"detail","purchase_token":"x"} {}`, `{"product_id":"detail","purchase_token":"x","status":"purchased"}`, `{"product_id":"detail","purchase_token":"bad/token"}`} {
		if _, err := v.Verify(context.Background(), []byte(proof)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted bad proof %s: %v", proof, err)
		}
	}
	if calls != 0 {
		t.Fatalf("looked up invalid proofs")
	}
	_, err := v.Verify(context.Background(), []byte(`{"product_id":"detail","purchase_token":"secret-token"}`))
	if !errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), "secret-token") {
		t.Fatalf("unsafe error: %v", err)
	}
	if _, err := NewStoreAdapter("google", "app.example", "production", nil, bindings); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil lookup: %v", err)
	}
}

func TestStoreAdapterNeverConvertsRefundOrPendingToPurchase(t *testing.T) {
	for _, status := range []Status{Pending, Cancelled, Refunded} {
		v, _ := NewStoreAdapter("apple", "app.example", "sandbox", lookupFunc(func(context.Context, StoreReference) (StoreSnapshot, error) {
			return StoreSnapshot{TransactionID: "1", ProductID: "detail", ApplicationID: "app.example", Environment: "sandbox", StoreAccountToken: "bound", Status: status, Revision: 1}, nil
		}), bindingsFunc(func(context.Context, string, string) (string, error) { return "user", nil }))
		got, err := v.Verify(context.Background(), []byte(`{"transaction_id":"1"}`))
		if err != nil || got.Status != status {
			t.Fatalf("status %s: %#v %v", status, got, err)
		}
	}
}

// Exercise the full verifier -> service -> repository path; a signed store
// result alone must not let another account or another reading claim a token.
func TestStoreAdapterServiceOwnershipAndProductBinding(t *testing.T) {
	ctx := context.Background()
	snapshot := StoreSnapshot{TransactionID: "123", ProductID: "detail", ApplicationID: "app.example", Environment: "sandbox", StoreAccountToken: "bound-alice", Status: Purchased, Revision: 1}
	lookup := lookupFunc(func(context.Context, StoreReference) (StoreSnapshot, error) { return snapshot, nil })
	bindings := bindingsFunc(func(_ context.Context, store, token string) (string, error) {
		if store == "apple" && token == "bound-alice" {
			return "alice", nil
		}
		if store == "apple" && token == "bound-bob" {
			return "bob", nil
		}
		return "", errors.New("unknown account binding")
	})
	adapter, err := NewStoreAdapter("apple", "app.example", "sandbox", lookup, bindings)
	if err != nil {
		t.Fatal(err)
	}
	repo := &mockRepo{owner: map[string]string{}, grants: map[string]bool{}}
	service := New(adapter, repo, "detail")
	proof := []byte(`{"transaction_id":"123"}`)
	if err = service.VerifyAndApply(ctx, "alice", "reading-1", proof); err != nil {
		t.Fatal(err)
	}
	if err = service.VerifyAndApply(ctx, "alice", "reading-1", proof); err != nil {
		t.Fatalf("duplicate must be idempotent: %v", err)
	}
	if err = service.VerifyAndApply(ctx, "alice", "reading-2", proof); !errors.Is(err, ErrInvalid) {
		t.Fatalf("same purchase applied to another reading: %v", err)
	}
	if err = service.VerifyAndApply(ctx, "bob", "reading-1", proof); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unbound account accepted: %v", err)
	}

	// Even if an authenticated store snapshot later points to another account,
	// an already-owned transaction cannot be transferred to that account.
	snapshot.StoreAccountToken = "bound-bob"
	snapshot.Revision = 2
	if err = service.VerifyAndApply(ctx, "bob", "reading-1", proof); !errors.Is(err, ErrOwnership) {
		t.Fatalf("transaction owner changed: %v", err)
	}
	if repo.grants["bob:reading-1"] {
		t.Fatal("other account received entitlement")
	}

	// A valid store record for an unrelated product is not a detail purchase.
	snapshot.ProductID = "unrelated-product"
	snapshot.StoreAccountToken = "bound-alice"
	if err = service.VerifyAndApply(ctx, "alice", "reading-1", proof); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unrelated product accepted: %v", err)
	}
	// An unresolved external account token is not the server account ID.
	snapshot.ProductID, snapshot.StoreAccountToken = "detail", "unknown-token"
	if err = service.VerifyAndApply(ctx, "alice", "reading-1", proof); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown binding accepted: %v", err)
	}
}

func TestStoreAdapterServiceRefundReconciliation(t *testing.T) {
	ctx := context.Background()
	snapshot := StoreSnapshot{TransactionID: "456", ProductID: "detail", ApplicationID: "app.example", Environment: "sandbox", StoreAccountToken: "bound", Status: Purchased, Revision: 1}
	adapter, err := NewStoreAdapter("google", "app.example", "sandbox", lookupFunc(func(context.Context, StoreReference) (StoreSnapshot, error) { return snapshot, nil }), bindingsFunc(func(context.Context, string, string) (string, error) { return "alice", nil }))
	if err != nil {
		t.Fatal(err)
	}
	repo := &mockRepo{owner: map[string]string{}, grants: map[string]bool{}}
	service := New(adapter, repo, "detail")
	proof := []byte(`{"product_id":"detail","purchase_token":"token-456"}`)
	if err = service.VerifyAndApply(ctx, "alice", "reading", proof); err != nil || !repo.grants["alice:reading"] {
		t.Fatalf("purchase: %v", err)
	}
	snapshot.Status, snapshot.Revision = Refunded, 2
	for i := 0; i < 2; i++ {
		if err = service.VerifyAndApply(ctx, "alice", "reading", proof); err != nil {
			t.Fatalf("refund replay: %v", err)
		}
	}
	if repo.grants["alice:reading"] {
		t.Fatal("refund did not revoke entitlement")
	}
	// Old purchase notification must not override a newer refund.
	snapshot.Status, snapshot.Revision = Purchased, 1
	if err = service.VerifyAndApply(ctx, "alice", "reading", proof); err != nil {
		t.Fatalf("stale replay: %v", err)
	}
	if repo.grants["alice:reading"] {
		t.Fatal("stale purchase restored a refunded entitlement")
	}
	// Conflicting statuses at an identical revision are not ordered safely.
	snapshot.Revision = 2
	if err = service.VerifyAndApply(ctx, "alice", "reading", proof); !errors.Is(err, ErrInvalid) {
		t.Fatalf("conflicting same-revision status accepted: %v", err)
	}
}

func TestStoreAdapterServiceConcurrentDuplicateTransaction(t *testing.T) {
	adapter, err := NewStoreAdapter("apple", "app.example", "sandbox", lookupFunc(func(context.Context, StoreReference) (StoreSnapshot, error) {
		return StoreSnapshot{TransactionID: "789", ProductID: "detail", ApplicationID: "app.example", Environment: "sandbox", StoreAccountToken: "bound", Status: Purchased, Revision: 1}, nil
	}), bindingsFunc(func(context.Context, string, string) (string, error) { return "alice", nil }))
	if err != nil {
		t.Fatal(err)
	}
	repo := &mockRepo{owner: map[string]string{}, grants: map[string]bool{}}
	service := New(adapter, repo, "detail")
	const count = 24
	results := make(chan error, count)
	var wait sync.WaitGroup
	for i := 0; i < count; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results <- service.VerifyAndApply(context.Background(), "alice", "reading", []byte(`{"transaction_id":"789"}`))
		}()
	}
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("duplicate transaction failed: %v", err)
		}
	}
	if !repo.grants["alice:reading"] || len(repo.revisions) != 1 {
		t.Fatal("duplicates did not yield one transaction entitlement")
	}
}
