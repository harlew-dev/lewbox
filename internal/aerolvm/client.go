package aerolvm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultRetryBudget = 60 * time.Second
	initialRetryWait   = 250 * time.Millisecond
	maxRetryWait       = 5 * time.Second
	errorBodyLimit     = 200
)

// Client talks to one AerolVM node's admin API with a bearer token.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
	retry   time.Duration
	sleep   func(time.Duration)
}

// Option changes a Client built by New.
type Option func(*Client)

// WithHTTPClient uses h for requests.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithRetry sets the total time spent retrying one request. Zero disables
// retries.
func WithRetry(max time.Duration) Option {
	return func(c *Client) {
		if max < 0 {
			max = 0
		}
		c.retry = max
	}
}

// WithSleep replaces the backoff sleep. Tests use it so retries do not wait.
func WithSleep(sleep func(time.Duration)) Option {
	return func(c *Client) {
		if sleep != nil {
			c.sleep = sleep
		}
	}
}

// New returns a client for the AerolVM at baseURL. token is sent as a bearer
// token on every route except /health.
func New(baseURL, token string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
		retry:   defaultRetryBudget,
		sleep:   time.Sleep,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// APIError is a non-2xx answer. Message is the API's reason when it gives
// one, otherwise its error code or the raw body.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("aerolvm: HTTP %d", e.Status)
	}
	return fmt.Sprintf("aerolvm: %s (HTTP %d)", e.Message, e.Status)
}

// Health calls GET /health. It needs no token.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var out Health
	err := c.do(ctx, http.MethodGet, "/health", nil, &out, false)
	return out, err
}

// WhoAmI calls GET /v1/whoami.
func (c *Client) WhoAmI(ctx context.Context) (WhoAmI, error) {
	var out WhoAmI
	err := c.do(ctx, http.MethodGet, "/v1/whoami", nil, &out, true)
	return out, err
}

// Members calls GET /v1/cluster/members.
func (c *Client) Members(ctx context.Context) (Members, error) {
	var out Members
	err := c.do(ctx, http.MethodGet, "/v1/cluster/members", nil, &out, true)
	return out, err
}

// CreateToken calls POST /v1/tokens.
func (c *Client) CreateToken(ctx context.Context, req CreateTokenRequest) (Token, error) {
	var out Token
	err := c.do(ctx, http.MethodPost, "/v1/tokens", req, &out, true)
	return out, err
}

// ListTokens calls GET /v1/tokens.
func (c *Client) ListTokens(ctx context.Context) ([]Token, error) {
	var out struct {
		Tokens []Token `json:"tokens"`
	}
	err := c.do(ctx, http.MethodGet, "/v1/tokens", nil, &out, true)
	return out.Tokens, err
}

// RevokeToken calls DELETE /v1/tokens/{id}.
func (c *Client) RevokeToken(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/tokens/"+url.PathEscape(id), nil, nil, true)
}

// CreateJoinToken calls POST /v1/cluster/join-tokens.
func (c *Client) CreateJoinToken(ctx context.Context, req JoinTokenRequest) (JoinToken, error) {
	var out JoinToken
	err := c.do(ctx, http.MethodPost, "/v1/cluster/join-tokens", req, &out, true)
	return out, err
}

// DrainNode calls POST /v1/cluster/nodes/{id}/drain.
func (c *Client) DrainNode(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodPost, "/v1/cluster/nodes/"+url.PathEscape(id)+"/drain", nil, nil, true)
}

// DeleteNode calls DELETE /v1/cluster/nodes/{id}.
func (c *Client) DeleteNode(ctx context.Context, id string, force bool) error {
	path := "/v1/cluster/nodes/" + url.PathEscape(id)
	if force {
		path += "?force=true"
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil, true)
}

// do sends one logical request, retrying the one 503 that usually clears up:
// a token write before raft has a leader.
func (c *Client) do(ctx context.Context, method, path string, body, out any, auth bool) error {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("aerolvm %s %s: encode request: %w", method, path, err)
		}
	}

	var waited time.Duration
	wait := initialRetryWait
	for {
		err := c.doOnce(ctx, method, path, payload, out, auth)
		if err == nil {
			return nil
		}
		if !retryable(err) || waited+wait > c.retry {
			return err
		}
		if ctx.Err() != nil {
			return err
		}
		c.sleep(wait)
		waited += wait
		if wait < maxRetryWait {
			wait *= 2
			if wait > maxRetryWait {
				wait = maxRetryWait
			}
		}
	}
}

func (c *Client) doOnce(ctx context.Context, method, path string, body []byte, out any, auth bool) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("aerolvm %s %s: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth && c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("aerolvm %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("aerolvm %s %s: read response: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return parseAPIError(resp.StatusCode, data)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("aerolvm %s %s: decode response: %w", method, path, err)
		}
	}
	return nil
}

func retryable(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusServiceUnavailable {
		return false
	}
	// "cluster: not enabled on this node" means this is a single-node
	// daemon answering a cluster route. It will never change on retry.
	return !strings.Contains(apiErr.Message, "not enabled")
}

func parseAPIError(status int, body []byte) *APIError {
	var payload struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	msg := ""
	if json.Unmarshal(body, &payload) == nil {
		if payload.Reason != "" {
			msg = payload.Reason
		} else {
			msg = payload.Error
		}
	}
	if msg == "" {
		msg = strings.TrimSpace(string(body))
		if len(msg) > errorBodyLimit {
			msg = msg[:errorBodyLimit]
		}
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	return &APIError{Status: status, Message: msg}
}
