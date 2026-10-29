package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// Mem0SourceCommit documents the OSS server contract inspected for this adapter.
const Mem0SourceCommit = "26cdefb46600c0fa5953c4b5ee5003c125539fc4"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type RemoteRecord struct {
	ID       string         `json:"id"`
	Memory   string         `json:"memory"`
	UserID   string         `json:"user_id"`
	Score    float64        `json:"score,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// Mem0 is an opt-in client for the self-hosted OSS REST server, not the hosted
// platform /v1 API. It never performs automatic extraction without Add(...,true).
type Mem0 struct {
	base   string
	apiKey string
	client *http.Client
}

func NewMem0(baseURL, apiKey string, client *http.Client) (*Mem0, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("%w: invalid Mem0 base URL", ErrInvalid)
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	if u.Scheme == "http" && host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("%w: use HTTPS for non-loopback Mem0 servers", ErrInvalid)
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("%w: Mem0 API key is required", ErrInvalid)
	}
	c := http.Client{Timeout: 30 * time.Second}
	if client != nil {
		c = *client
		if c.Timeout == 0 {
			c.Timeout = 30 * time.Second
		}
	}
	// X-API-Key must never follow a redirect to a different host.
	c.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Mem0{base: strings.TrimRight(baseURL, "/"), apiKey: apiKey, client: &c}, nil
}

// Add sends explicitly selected messages. infer=false stores messages verbatim;
// infer=true asks Mem0's configured LLM to extract memories and may incur cost.
// This response is Mem0's event result, not a factual accuracy verdict.
func (m *Mem0) Add(ctx context.Context, userID string, messages []Message, infer bool) (json.RawMessage, error) {
	if err := validateUser(userID); err != nil {
		return nil, err
	}
	if len(messages) == 0 || len(messages) > 100 {
		return nil, fmt.Errorf("%w: provide 1..100 messages", ErrInvalid)
	}
	for _, msg := range messages {
		if (msg.Role != "user" && msg.Role != "assistant") || strings.TrimSpace(msg.Content) == "" || len(msg.Content) > 16384 {
			return nil, fmt.Errorf("%w: invalid message", ErrInvalid)
		}
	}
	return m.request(ctx, http.MethodPost, "/memories", map[string]any{"user_id": userID, "messages": messages, "infer": infer})
}

func (m *Mem0) Search(ctx context.Context, userID, query string, topK int) ([]RemoteRecord, error) {
	if err := validateUser(userID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query) == "" || len(query) > 16384 || topK < 1 || topK > 100 {
		return nil, fmt.Errorf("%w: query required; top_k must be 1..100", ErrInvalid)
	}
	data, err := m.request(ctx, http.MethodPost, "/search", map[string]any{"query": query, "filters": map[string]string{"user_id": userID}, "top_k": topK, "show_expired": false})
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Results []RemoteRecord `json:"results"`
	}
	if err = json.Unmarshal(data, &envelope); err != nil {
		return nil, fmt.Errorf("decode Mem0 search: %w", err)
	}
	for _, r := range envelope.Results {
		if r.UserID != userID {
			return nil, ErrScope
		}
	}
	if envelope.Results == nil {
		envelope.Results = []RemoteRecord{}
	}
	return envelope.Results, nil
}

var safeMemoryID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Get checks the stored owner, rather than treating knowledge of an ID as access.
func (m *Mem0) Get(ctx context.Context, userID, id string) (RemoteRecord, error) {
	if err := validateUser(userID); err != nil {
		return RemoteRecord{}, err
	}
	if !safeMemoryID.MatchString(id) {
		return RemoteRecord{}, fmt.Errorf("%w: invalid memory ID", ErrInvalid)
	}
	data, err := m.request(ctx, http.MethodGet, "/memories/"+id, nil)
	if err != nil {
		return RemoteRecord{}, err
	}
	var r RemoteRecord
	if err = json.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("decode Mem0 record: %w", err)
	}
	if r.ID == "" {
		return RemoteRecord{}, ErrNotFound
	}
	if r.UserID != userID {
		return RemoteRecord{}, ErrScope
	}
	return r, nil
}

func (m *Mem0) Update(ctx context.Context, userID, id, text string) (json.RawMessage, error) {
	if strings.TrimSpace(text) == "" || len(text) > 16384 {
		return nil, fmt.Errorf("%w: nonempty text is required", ErrInvalid)
	}
	if _, err := m.Get(ctx, userID, id); err != nil {
		return nil, err
	}
	// No metadata updates: callers cannot reassign a memory's owner through here.
	return m.request(ctx, http.MethodPut, "/memories/"+id, map[string]string{"text": text})
}

func (m *Mem0) Delete(ctx context.Context, userID, id string) error {
	if _, err := m.Get(ctx, userID, id); err != nil {
		return err
	}
	_, err := m.request(ctx, http.MethodDelete, "/memories/"+id, nil)
	return err
}

// DeleteUser is intentionally scoped. The upstream endpoint requires an admin
// key; a per-user account API key may return 403. It does not call global /reset.
func (m *Mem0) DeleteUser(ctx context.Context, userID string) error {
	if err := validateUser(userID); err != nil {
		return err
	}
	_, err := m.request(ctx, http.MethodDelete, "/memories?"+url.Values{"user_id": []string{userID}}.Encode(), nil)
	return err
}

type HTTPError struct{ StatusCode int }

func (e *HTTPError) Error() string { return fmt.Sprintf("Mem0 returned HTTP %d", e.StatusCode) }

func (m *Mem0) request(ctx context.Context, method, path string, payload any) (json.RawMessage, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, m.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", m.apiKey)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Mem0 request failed: %w", err)
	}
	defer res.Body.Close()
	// Do not surface upstream bodies: they can echo private facts or credentials.
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, &HTTPError{StatusCode: res.StatusCode}
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("Mem0 response exceeds 1 MiB")
	}
	if len(data) == 0 && res.StatusCode == http.StatusNoContent {
		return json.RawMessage(`{}`), nil
	}
	if !json.Valid(data) {
		return nil, fmt.Errorf("Mem0 response is not JSON")
	}
	return json.RawMessage(data), nil
}
