package purchase

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"regexp"
	"strings"
)

// StoreLookup is the boundary to an authenticated store SDK/client. It MUST
// query authoritative current state, verify the provider's response and return
// a provider-specific normalized snapshot. This package does not implement a
// StoreKit signature verifier, Google OAuth, or an HTTP store client.
// Revision must be a durable monotonic per-transaction reconciliation version;
// wall-clock time or notification delivery order alone does not meet this rule.
type StoreLookup interface {
	Lookup(ctx context.Context, reference StoreReference) (StoreSnapshot, error)
}

// AccountBindings resolves a store-authenticated account token to the server's
// user. The token must be generated and bound by the server before purchase.
type AccountBindings interface {
	Resolve(ctx context.Context, store, storeAccountToken string) (string, error)
}

// StoreReference is an untrusted pointer only. ProductID and PurchaseToken are
// used for Google; Apple uses TransactionID. Never log the purchase token.
type StoreReference struct {
	TransactionID string `json:"transaction_id,omitempty"`
	ProductID     string `json:"product_id,omitempty"`
	PurchaseToken string `json:"purchase_token,omitempty"`
}

// StoreSnapshot is returned by the trusted SDK/client after authenticating the
// provider and reconciling current state. StoreAccountToken MUST be taken from
// authenticated provider data, never echoed from the request.
type StoreSnapshot struct {
	TransactionID     string
	ProductID         string
	ApplicationID     string
	Environment       string
	StoreAccountToken string
	Status            Status
	Revision          uint64
}

// StoreAdapter validates store-supplied results and resolves account ownership.
// It is a dependency boundary, not a complete production store integration.
type StoreAdapter struct {
	store, applicationID, environment string
	lookup                            StoreLookup
	bindings                          AccountBindings
}

func NewStoreAdapter(store, applicationID, environment string, lookup StoreLookup, bindings AccountBindings) (*StoreAdapter, error) {
	if (store != "apple" && store != "google") || strings.TrimSpace(applicationID) == "" || len(applicationID) > 255 || (environment != "production" && environment != "sandbox") || lookup == nil || bindings == nil {
		return nil, ErrUnavailable
	}
	return &StoreAdapter{store: store, applicationID: applicationID, environment: environment, lookup: lookup, bindings: bindings}, nil
}

var appleTransactionID = regexp.MustCompile(`^[0-9]{1,32}$`)
var googleProductID = regexp.MustCompile(`^[A-Za-z0-9._-]{1,255}$`)
var googleToken = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

func parseStoreReference(store string, proof []byte) (StoreReference, error) {
	var ref StoreReference
	if len(proof) == 0 || len(proof) > 4096 {
		return ref, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(proof))
	d.DisallowUnknownFields()
	if d.Decode(&ref) != nil {
		return ref, ErrInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ref, ErrInvalid
	}
	if store == "apple" {
		if !appleTransactionID.MatchString(ref.TransactionID) || ref.ProductID != "" || ref.PurchaseToken != "" {
			return StoreReference{}, ErrInvalid
		}
	} else if store == "google" {
		if ref.TransactionID != "" || !googleProductID.MatchString(ref.ProductID) || len(ref.PurchaseToken) > 2048 || !googleToken.MatchString(ref.PurchaseToken) {
			return StoreReference{}, ErrInvalid
		}
	} else {
		return StoreReference{}, ErrInvalid
	}
	return ref, nil
}

func (a *StoreAdapter) Verify(ctx context.Context, proof []byte) (Verified, error) {
	if a == nil || a.lookup == nil || a.bindings == nil {
		return Verified{}, ErrUnavailable
	}
	ref, err := parseStoreReference(a.store, proof)
	if err != nil {
		return Verified{}, err
	}
	s, err := a.lookup.Lookup(ctx, ref)
	if err != nil {
		// Avoid including provider errors that may contain purchase tokens.
		return Verified{}, ErrUnavailable
	}
	if s.ApplicationID != a.applicationID || s.Environment != a.environment || s.TransactionID == "" || len(s.TransactionID) > 512 || s.ProductID == "" || len(s.ProductID) > 255 || s.StoreAccountToken == "" || len(s.StoreAccountToken) > 255 || s.Revision == 0 {
		return Verified{}, ErrInvalid
	}
	if a.store == "apple" && s.TransactionID != ref.TransactionID || a.store == "google" && s.ProductID != ref.ProductID {
		return Verified{}, ErrInvalid
	}
	switch s.Status {
	case Pending, Purchased, Cancelled, Refunded:
	default:
		return Verified{}, ErrInvalid
	}
	user, err := a.bindings.Resolve(ctx, a.store, s.StoreAccountToken)
	if err != nil || strings.TrimSpace(user) == "" {
		return Verified{}, ErrInvalid
	}
	return Verified{Store: a.store, TransactionID: s.TransactionID, ProductID: s.ProductID, AccountID: user, Status: s.Status, Revision: s.Revision}, nil
}
