// Package completions asks a code model for the text at the cursor
// (fill-in-the-middle: the code before and after it): Ollama's /api/generate
// with a suffix, or an OpenAI-compatible /v1/completions endpoint. Plain http
// is allowed only to this machine; anything else needs https, and what goes
// to a remote endpoint has its credentials redacted first.
package completions

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"syscall"
	"time"

	"flint/internal/redact"
)

// Providers.
const (
	Ollama = "ollama"
	OpenAI = "openai"
)

// Security limits (constants, not settings): the size of what is sent and
// read back, and how long one request may take (the first one loads the
// model).
const (
	MaxContextBytes  = 64 << 10 // prefix and suffix each
	maxResponseBytes = 1 << 20
	maxEndpointLen   = 256
	maxModelLen      = 200
	requestTimeout   = 30 * time.Second
	temperature      = 0.2
)

// Config is the completion setup from settings.
type Config struct {
	Provider  string
	Endpoint  string
	Model     string
	MaxTokens int
}

// Request is the code around the cursor in the file at Path.
type Request struct {
	Path   string `json:"path"`
	Prefix string `json:"prefix"`
	Suffix string `json:"suffix"`
}

// Client sends completion requests; HTTP never follows redirects (an API key
// or code must not end up somewhere else than the configured endpoint).
type Client struct {
	HTTP *http.Client
}

// New returns a client with Flint's timeout and redirect policy.
func New() *Client {
	return &Client{HTTP: &http.Client{
		Timeout:       requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// ValidProvider reports whether p is a supported provider.
func ValidProvider(p string) bool { return p == Ollama || p == OpenAI }

// CheckEndpoint accepts an http(s) base URL without credentials, query or
// fragment; plain http only for localhost, 127.0.0.1 and ::1.
func CheckEndpoint(raw string) error {
	if raw == "" {
		return errors.New("endpoint is empty")
	}
	if len(raw) > maxEndpointLen {
		return fmt.Errorf("endpoint longer than %d characters", maxEndpointLen)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid endpoint %q", raw)
	}
	switch {
	case u.Scheme != "http" && u.Scheme != "https":
		return fmt.Errorf("endpoint %q must start with http:// or https://", raw)
	case u.User != nil:
		return errors.New("the endpoint may not contain credentials; store an API key instead")
	case u.RawQuery != "" || u.Fragment != "" || u.ForceQuery || strings.Contains(raw, "#"):
		return errors.New("the endpoint may not have a query or fragment")
	case u.Hostname() == "":
		return fmt.Errorf("endpoint %q has no host", raw)
	case u.Scheme == "http" && !isLoopback(u.Hostname()):
		return fmt.Errorf("endpoint %q: use https for anything but localhost, 127.0.0.1 and ::1", raw)
	}
	return nil
}

// IsLocal reports whether endpoint is on this machine.
func IsLocal(endpoint string) bool {
	u, err := url.Parse(endpoint)
	return err == nil && isLoopback(u.Hostname())
}

func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

var modelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]*$`)

// CheckModel accepts a model name such as qwen2.5-coder:1.5b-base.
func CheckModel(model string) error {
	if model == "" || len(model) > maxModelLen || !modelPattern.MatchString(model) {
		return fmt.Errorf("invalid model %q", model)
	}
	return nil
}

func (c Config) check() error {
	if !ValidProvider(c.Provider) {
		return fmt.Errorf("unsupported provider %q", c.Provider)
	}
	if err := CheckEndpoint(c.Endpoint); err != nil {
		return err
	}
	if c.MaxTokens < 1 {
		return errors.New("maxTokens must be positive")
	}
	return CheckModel(c.Model)
}

// Complete returns the model's text for the cursor ("" when it has none, and
// without asking for a file that holds credentials).
// Cancelling ctx (the user typed on) aborts the request. apiKey, when set, is
// sent as a bearer token.
func (c *Client) Complete(ctx context.Context, cfg Config, apiKey string, r Request) (string, error) {
	if err := cfg.check(); err != nil {
		return "", err
	}
	if redact.SecretFile(r.Path) {
		return "", nil // .env, keys, kubeconfig: never sent anywhere
	}
	if len(r.Prefix) > MaxContextBytes || len(r.Suffix) > MaxContextBytes {
		return "", fmt.Errorf("the code around the cursor is larger than %d KB", MaxContextBytes>>10)
	}
	if !IsLocal(cfg.Endpoint) {
		r = Request{Path: r.Path, Prefix: redact.Secrets(r.Prefix), Suffix: redact.Secrets(r.Suffix)}
	}
	req, err := buildRequest(ctx, cfg, apiKey, r)
	if err != nil {
		return "", err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", transportError(ctx, cfg, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return "", transportError(ctx, cfg, err)
	}
	if len(body) > maxResponseBytes {
		return "", fmt.Errorf("the response of %s is too large", cfg.Endpoint)
	}
	return parseResponse(cfg, resp.StatusCode, body)
}

func buildRequest(ctx context.Context, cfg Config, apiKey string, r Request) (*http.Request, error) {
	base := strings.TrimRight(cfg.Endpoint, "/")
	var target string
	var payload any
	switch cfg.Provider {
	case Ollama:
		target = base + "/api/generate"
		suffix := r.Suffix
		if suffix == "" {
			// Ollama fills in the middle only with a suffix; without one a
			// coder model's template falls back to chat.
			suffix = "\n"
		}
		payload = map[string]any{
			"model":  cfg.Model,
			"prompt": r.Prefix,
			"suffix": suffix,
			"stream": false,
			"options": map[string]any{
				"num_predict": cfg.MaxTokens,
				"temperature": temperature,
			},
		}
	case OpenAI:
		if strings.HasSuffix(base, "/v1") {
			target = base + "/completions"
		} else {
			target = base + "/v1/completions"
		}
		payload = map[string]any{
			"model":       cfg.Model,
			"prompt":      r.Prefix,
			"suffix":      r.Suffix,
			"max_tokens":  cfg.MaxTokens,
			"temperature": temperature,
			"stream":      false,
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	return req, nil
}

func transportError(ctx context.Context, cfg Config, err error) error {
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return ctx.Err()
	case errors.Is(err, syscall.ECONNREFUSED) && cfg.Provider == Ollama:
		return fmt.Errorf("Ollama is not running at %s (start it with: ollama serve)", cfg.Endpoint)
	case errors.Is(err, syscall.ECONNREFUSED):
		return fmt.Errorf("nothing answers at %s", cfg.Endpoint)
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || isTimeout(err):
		return fmt.Errorf("%s did not answer in time", cfg.Endpoint)
	}
	return fmt.Errorf("%s: %s", cfg.Endpoint, redact.String(err.Error()))
}

func isTimeout(err error) bool {
	var t interface{ Timeout() bool }
	return errors.As(err, &t) && t.Timeout()
}

// response covers both providers' answers and errors.
type response struct {
	Response string `json:"response"` // Ollama
	Choices  []struct {
		Text string `json:"text"`
	} `json:"choices"` // OpenAI
	Error json.RawMessage `json:"error"` // a string (Ollama) or {"message": …} (OpenAI)
}

const maxErrorShown = 300

func parseResponse(cfg Config, status int, body []byte) (string, error) {
	var r response
	jsonErr := json.Unmarshal(body, &r)
	if status < 200 || status > 299 {
		msg := errorMessage(r.Error)
		if jsonErr != nil || msg == "" {
			msg = http.StatusText(status)
		}
		if cfg.Provider == Ollama && status == http.StatusNotFound && strings.Contains(msg, "not found") {
			return "", fmt.Errorf("model %s not found: run ollama pull %s", cfg.Model, cfg.Model)
		}
		return "", fmt.Errorf("%s answered %d: %s", cfg.Endpoint, status, shorten(redact.String(msg)))
	}
	if jsonErr != nil {
		return "", fmt.Errorf("invalid response from %s", cfg.Endpoint)
	}
	if msg := errorMessage(r.Error); msg != "" {
		return "", fmt.Errorf("%s: %s", cfg.Endpoint, shorten(redact.String(msg)))
	}
	text := r.Response
	if cfg.Provider == OpenAI {
		if len(r.Choices) == 0 {
			return "", nil
		}
		text = r.Choices[0].Text
	}
	return strings.TrimRight(text, " \t\r\n"), nil
}

func errorMessage(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var obj struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Message
	}
	return ""
}

func shorten(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > maxErrorShown {
		return s[:maxErrorShown] + "…"
	}
	return s
}
