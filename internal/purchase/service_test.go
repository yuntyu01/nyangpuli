package purchase

import (
	"context"
	"errors"
	"sync"
	"testing"
)

type verifierFunc func(context.Context, []byte) (Verified, error)

func (f verifierFunc) Verify(c context.Context, p []byte) (Verified, error) { return f(c, p) }

type mockRepo struct {
	mu        sync.Mutex
	owner     map[string]string
	grants    map[string]bool
	revisions map[string]uint64
	statuses  map[string]Status
}

func (r *mockRepo) Apply(_ context.Context, user, reading string, p Verified) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := p.Store + ":" + p.TransactionID
	if old := r.owner[key]; old != "" && old != user {
		return ErrOwnership
	}
	// A transaction is tied to one reading as well as one account.
	if old := r.owner[key+":reading"]; old != "" && old != reading {
		return ErrInvalid
	}
	if r.revisions == nil {
		r.revisions = map[string]uint64{}
		r.statuses = map[string]Status{}
	}
	if p.Revision < r.revisions[key] {
		return nil
	}
	if p.Revision == r.revisions[key] && r.statuses[key] != p.Status {
		return ErrInvalid
	}
	r.revisions[key], r.statuses[key] = p.Revision, p.Status
	r.owner[key] = user
	r.owner[key+":reading"] = reading
	r.grants[user+":"+reading] = p.Status == Purchased
	return nil
}
func TestVerifiedPurchaseAndRefund(t *testing.T) {
	ctx := context.Background()
	status := Purchased
	revision := uint64(1)
	verifier := verifierFunc(func(context.Context, []byte) (Verified, error) {
		return Verified{Store: "mock", TransactionID: "tx", ProductID: "detail", AccountID: "alice", Status: status, Revision: revision}, nil
	})
	r := &mockRepo{owner: map[string]string{}, grants: map[string]bool{}}
	s := New(verifier, r, "detail")
	for i := 0; i < 2; i++ {
		if err := s.VerifyAndApply(ctx, "alice", "reading", []byte("signed proof")); err != nil {
			t.Fatal(err)
		}
	}
	if !r.grants["alice:reading"] {
		t.Fatal("verified purchase not granted")
	}
	if err := s.VerifyAndApply(ctx, "alice", "other", []byte("proof")); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if err := s.VerifyAndApply(ctx, "bob", "reading", []byte("proof")); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	status = Refunded
	revision = 2
	for i := 0; i < 2; i++ {
		if err := s.VerifyAndApply(ctx, "alice", "reading", []byte("proof")); err != nil {
			t.Fatal(err)
		}
	}
	if r.grants["alice:reading"] {
		t.Fatal("refund did not revoke")
	}
	status, revision = Purchased, 1
	if err := s.VerifyAndApply(ctx, "alice", "reading", []byte("old notification")); err != nil {
		t.Fatal(err)
	}
	if r.grants["alice:reading"] {
		t.Fatal("stale event re-granted refund")
	}
}
func TestFailClosedWithoutVerifierAndNonPurchasedStates(t *testing.T) {
	ctx := context.Background()
	if err := New(nil, nil, "detail").VerifyAndApply(ctx, "alice", "reading", []byte("proof")); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	for _, status := range []Status{Pending, Cancelled, Refunded} {
		t.Run(string(status), func(t *testing.T) {
			r := &mockRepo{owner: map[string]string{}, grants: map[string]bool{}}
			s := New(verifierFunc(func(context.Context, []byte) (Verified, error) {
				return Verified{Store: "mock", TransactionID: "tx", ProductID: "detail", AccountID: "alice", Status: status, Revision: 1}, nil
			}), r, "detail")
			if err := s.VerifyAndApply(ctx, "alice", "reading", []byte("proof")); err != nil {
				t.Fatal(err)
			}
			if r.grants["alice:reading"] {
				t.Fatal("non-purchased status granted")
			}
		})
	}
}
