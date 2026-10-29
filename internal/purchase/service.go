// Package purchase defines server-verified purchase boundaries. The verifier
// must authenticate the store response; client-provided success is never proof.
package purchase

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrUnavailable = errors.New("store verification is not configured")
	ErrInvalid     = errors.New("invalid purchase")
	ErrOwnership   = errors.New("transaction belongs to another user")
)

type Status string

const (
	Pending   Status = "pending"
	Purchased Status = "purchased"
	Cancelled Status = "cancelled"
	Refunded  Status = "refunded"
)

// Verified must come exclusively from the server's store adapter. Binding must
// be derived from store-verified account metadata, never from an HTTP body.
// Status must be the current authoritative store status, not merely the status
// in an old purchase receipt or notification. Implementations must verify or
// query the store before returning, including for restore and notification.
type Verified struct {
	Store         string
	TransactionID string
	ProductID     string
	AccountID     string
	Status        Status
	// Revision is assigned by the verifier's reconciled store state. The
	// production repository must compare it atomically per transaction.
	Revision uint64
}

type Verifier interface {
	Verify(ctx context.Context, proof []byte) (Verified, error)
}

// Repository.Apply is an atomic transaction: it must reject assignment of a
// store/transaction pair to another user, apply refunds/revocations idempotently,
// and never grant rights for statuses other than Purchased. It must prevent a
// stale verification from overriding a newer refund or revocation (e.g. using
// store revisions and transactional ordering). Repository records should contain
// no raw receipt or sensitive reading contents.
type Repository interface {
	Apply(ctx context.Context, userID, readingID string, purchase Verified) error
}

type Service struct {
	verifier   Verifier
	repository Repository
	productID  string
}

func New(verifier Verifier, repository Repository, productID string) *Service {
	return &Service{verifier: verifier, repository: repository, productID: productID}
}

// VerifyAndApply receives a server-authenticated user ID. There is no insecure
// fallback when a store or durable repository is absent.
func (s *Service) VerifyAndApply(ctx context.Context, userID, readingID string, proof []byte) error {
	if s == nil || s.verifier == nil || s.repository == nil {
		return ErrUnavailable
	}
	if strings.TrimSpace(userID) == "" || strings.TrimSpace(readingID) == "" || len(proof) == 0 || len(proof) > 256*1024 || s.productID == "" {
		return ErrInvalid
	}
	verified, err := s.verifier.Verify(ctx, proof)
	if err != nil {
		return err
	}
	if verified.Store == "" || verified.TransactionID == "" || verified.ProductID != s.productID || verified.AccountID != userID || verified.Revision == 0 {
		return ErrInvalid
	}
	switch verified.Status {
	case Pending, Purchased, Cancelled, Refunded:
	default:
		return ErrInvalid
	}
	return s.repository.Apply(ctx, userID, readingID, verified)
}
