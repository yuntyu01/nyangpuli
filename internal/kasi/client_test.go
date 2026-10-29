package kasi

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestConstrainedTransportWithSyntheticResponse(t *testing.T) {
	const key = "private-test-key"
	transport := roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "apis.data.go.kr" || r.URL.Query().Get("secret") != key || r.URL.Query().Get("year") != "2026" {
			t.Fatal("incorrect request")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("synthetic response")), Header: make(http.Header)}, nil
	})
	c, err := New("https://apis.data.go.kr/example", "secret", key, transport)
	if err != nil {
		t.Fatal(err)
	}
	body, err := c.Fetch(context.Background(), url.Values{"year": {"2026"}})
	if err != nil || string(body) != "synthetic response" {
		t.Fatalf("unexpected result: %v", err)
	}
	if _, err = c.Fetch(context.Background(), url.Values{"secret": {"override"}}); err == nil {
		t.Fatal("client credential override accepted")
	}
}
func TestRejectsInvalidConfigurationAndOversizedResponse(t *testing.T) {
	if _, err := New("http://apis.data.go.kr/example", "secret", "key", nil); err == nil {
		t.Fatal("http allowed")
	}
	if _, err := New("https://other.example/example", "secret", "key", nil); err == nil {
		t.Fatal("wrong host allowed")
	}
	c, err := New("https://apis.data.go.kr/example", "secret", "key", roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", MaxResponseBytes+1))), Header: make(http.Header)}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Fetch(context.Background(), nil); err == nil {
		t.Fatal("oversized response accepted")
	}
}
