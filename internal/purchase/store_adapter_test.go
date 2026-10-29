package purchase

import (
	"context"
	"errors"
	"strings"
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
