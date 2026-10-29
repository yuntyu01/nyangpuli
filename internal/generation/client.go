// Package generation provides an opt-in, text-only Chat Completions adapter.
// It never selects a model, judges answers, or treats generated text as ground truth.
package generation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Input struct {
	CaseID    string `json:"case_id"`
	VariantID string `json:"variant_id"`
	Question  string `json:"question"`
	// CaseContext contains only the shared original inputs needed by reviewers,
	// such as facts, drawn cards and positions. Never put condition evidence here.
	CaseContext  json.RawMessage `json:"case_context,omitempty"`
	SystemPrompt string          `json:"system_prompt"`
	UserPrompt   string          `json:"user_prompt"`
	Sources      []string        `json:"sources,omitempty"`
}

type Config struct {
	APIBase             string   `json:"api_base"`
	APIKey              string   `json:"-"`
	Model               string   `json:"model"`
	MaxCompletionTokens int      `json:"max_completion_tokens"`
	Temperature         *float64 `json:"temperature,omitempty"`
}

// ConfigFromEnv does not make requests. There are deliberately no provider or
// model defaults. APIBase is the base including /v1 where the provider uses it.
func ConfigFromEnv() (Config, error) {
	c := Config{APIBase: strings.TrimSpace(os.Getenv("NYANG_LLM_API_BASE")), APIKey: os.Getenv("NYANG_LLM_API_KEY"), Model: strings.TrimSpace(os.Getenv("NYANG_LLM_MODEL")), MaxCompletionTokens: 1024}
	if v := os.Getenv("NYANG_LLM_MAX_TOKENS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return Config{}, errors.New("NYANG_LLM_MAX_TOKENS must be an integer")
		}
		c.MaxCompletionTokens = n
	}
	if v := os.Getenv("NYANG_LLM_TEMPERATURE"); v != "" {
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return Config{}, errors.New("NYANG_LLM_TEMPERATURE must be numeric")
		}
		c.Temperature = &n
	}
	return c, validateConfig(c)
}

func validateConfig(c Config) error {
	u, err := url.Parse(c.APIBase)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("API base must be an absolute URL without credentials, query, or fragment")
	}
	loopback := u.Hostname() == "localhost" || (net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback())
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return errors.New("API base requires HTTPS, except a loopback development server")
	}
	if strings.TrimSpace(c.Model) == "" {
		return errors.New("an explicit model is required")
	}
	if !loopback && strings.TrimSpace(c.APIKey) == "" {
		return errors.New("API key is required for a remote provider")
	}
	if c.MaxCompletionTokens < 1 || c.MaxCompletionTokens > 32768 {
		return errors.New("max completion tokens must be between 1 and 32768")
	}
	if c.Temperature != nil && !(*c.Temperature >= 0 && *c.Temperature <= 2) {
		return errors.New("temperature must be between 0 and 2")
	}
	return nil
}

type Client struct {
	config   Config
	http     *http.Client
	endpoint string
}

// New copies the client and disables redirects so a provider redirect cannot
// silently send prompts or credentials to another destination.
func New(config Config, client *http.Client) (*Client, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: 90 * time.Second}
	}
	copied := *client
	if copied.Timeout == 0 {
		copied.Timeout = 90 * time.Second
	}
	copied.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{config: config, http: &copied, endpoint: strings.TrimRight(config.APIBase, "/") + "/chat/completions"}, nil
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
type Result struct {
	Input             Input     `json:"input"`
	Status            string    `json:"status"`
	Text              string    `json:"text,omitempty"`
	PromptSHA256      string    `json:"prompt_sha256"`
	Config            *Config   `json:"config,omitempty"`
	ReturnedModel     string    `json:"returned_model,omitempty"`
	SystemFingerprint string    `json:"system_fingerprint,omitempty"`
	ResponseID        string    `json:"response_id,omitempty"`
	FinishReason      string    `json:"finish_reason,omitempty"`
	Usage             Usage     `json:"usage"`
	StartedAt         time.Time `json:"started_at"`
	DurationMS        int64     `json:"duration_ms"`
	Error             string    `json:"error,omitempty"`
}

func promptHash(in Input) string {
	b, _ := json.Marshal(struct{ System, User string }{in.SystemPrompt, in.UserPrompt})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func validateInputs(inputs []Input) error {
	if len(inputs) == 0 {
		return errors.New("at least one input is required")
	}
	seen := map[string]bool{}
	for _, in := range inputs {
		if len(in.CaseContext) > 0 && !json.Valid(in.CaseContext) {
			return errors.New("case_context must be valid JSON")
		}
		if strings.TrimSpace(in.CaseID) == "" || strings.TrimSpace(in.VariantID) == "" || strings.TrimSpace(in.Question) == "" || strings.TrimSpace(in.UserPrompt) == "" {
			return errors.New("case_id, variant_id, question and user_prompt are required")
		}
		key := in.CaseID + "\x00" + in.VariantID
		if seen[key] {
			return errors.New("duplicate case/variant input")
		}
		seen[key] = true
	}
	return nil
}

// Run performs no network request unless enabled is explicitly true. It stops
// at the first provider failure, preserving preceding results for audit. It
// never retries (a retry could cause another charged generation).
func Run(ctx context.Context, inputs []Input, client *Client, enabled bool) ([]Result, error) {
	if err := validateInputs(inputs); err != nil {
		return nil, err
	}
	if enabled && client == nil {
		return nil, errors.New("enabled generation requires a configured client")
	}
	results := make([]Result, 0, len(inputs))
	for _, in := range inputs {
		r := Result{Input: in, Status: "not_run", PromptSHA256: promptHash(in)}
		if !enabled {
			results = append(results, r)
			continue
		}
		r.StartedAt = time.Now().UTC()
		config := client.config
		config.APIKey = ""
		r.Config = &config
		err := client.generate(ctx, in, &r)
		r.DurationMS = time.Since(r.StartedAt).Milliseconds()
		if err != nil {
			r.Status = "failed"
			r.Error = err.Error()
			results = append(results, r)
			return results, err
		}
		r.Status = "generated"
		results = append(results, r)
	}
	return results, nil
}

func (c *Client) generate(ctx context.Context, in Input, out *Result) error {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	messages := []message{}
	if in.SystemPrompt != "" {
		messages = append(messages, message{"system", in.SystemPrompt})
	}
	messages = append(messages, message{"user", in.UserPrompt})
	payload := struct {
		Model               string    `json:"model"`
		Messages            []message `json:"messages"`
		MaxCompletionTokens int       `json:"max_completion_tokens"`
		Temperature         *float64  `json:"temperature,omitempty"`
		Store               bool      `json:"store"`
	}{c.config.Model, messages, c.config.MaxCompletionTokens, c.config.Temperature, false}
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.New("cannot encode provider request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("cannot construct provider request")
	}
	req.Header.Set("Content-Type", "application/json")
	if c.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("provider transport failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("provider returned HTTP %d (response body withheld)", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if err != nil {
		return errors.New("cannot read provider response")
	}
	if len(b) > 2*1024*1024 {
		return errors.New("provider response exceeds 2 MiB")
	}
	var parsed struct {
		ID          string `json:"id"`
		Model       string `json:"model"`
		Fingerprint string `json:"system_fingerprint"`
		Usage       Usage  `json:"usage"`
		Choices     []struct {
			Message struct {
				Content *string `json:"content"`
				Refusal *string `json:"refusal"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if json.Unmarshal(b, &parsed) != nil {
		return errors.New("invalid provider JSON")
	}
	if len(parsed.Choices) != 1 {
		return errors.New("expected exactly one provider choice")
	}
	choice := parsed.Choices[0]
	out.ResponseID = parsed.ID
	out.ReturnedModel = parsed.Model
	out.SystemFingerprint = parsed.Fingerprint
	out.FinishReason = choice.FinishReason
	out.Usage = parsed.Usage
	if choice.Message.Refusal != nil && *choice.Message.Refusal != "" {
		return errors.New("provider refused the request")
	}
	if choice.Message.Content == nil || strings.TrimSpace(*choice.Message.Content) == "" {
		return errors.New("provider returned no text")
	}
	out.Text = *choice.Message.Content
	if choice.FinishReason != "stop" {
		return fmt.Errorf("incomplete provider answer (finish_reason=%s)", choice.FinishReason)
	}
	return nil
}
