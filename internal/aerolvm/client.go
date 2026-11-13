package aerolvm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultCallTimeout bounds one attempt when the caller's context has no
	// deadline. A caller that sets one gets it in full: a Firecracker stop
	// writes the VM's memory to disk and can take minutes.
	defaultCallTimeout = 30 * time.Second
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
	// callTimeout bounds one attempt when the caller set no deadline.
	callTimeout time.Duration
}

// Option changes a Client built by New.
type Option func(*Client)

// WithHTTPClient uses h for requests.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithCallTimeout sets how long one attempt may take when the caller's
// context has no deadline (default 30 s).
func WithCallTimeout(d time.Duration) Option {
	return func(c *Client) { c.callTimeout = d }
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
		baseURL:     strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:       token,
		http:        &http.Client{},
		callTimeout: defaultCallTimeout,
		retry:       defaultRetryBudget,
		sleep:       time.Sleep,
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

// NodeCertExpiry reads the node's cluster certificate expiry from the
// aerolvm_cluster_mtls_cert_not_after_unix gauge on GET /v1/metrics. The bool
// is false when the node isn't in cluster mode (the gauge is absent or 0).
func (c *Client) NodeCertExpiry(ctx context.Context) (time.Time, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/metrics", nil)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("aerolvm GET /v1/metrics: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("aerolvm GET /v1/metrics: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("aerolvm GET /v1/metrics: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return time.Time{}, false, parseAPIError(resp.StatusCode, data)
	}
	const gauge = "aerolvm_cluster_mtls_cert_not_after_unix"
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != gauge {
			continue
		}
		seconds, err := strconv.ParseFloat(fields[1], 64)
		if err != nil || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 {
			return time.Time{}, false, nil
		}
		return time.Unix(int64(seconds), 0).UTC(), true, nil
	}
	return time.Time{}, false, nil
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

// ListSandboxes reads every page of GET /v1/sandboxes. It reports whether any
// page was partial, which means a node did not answer.
func (c *Client) ListSandboxes(ctx context.Context) ([]Sandbox, bool, error) {
	all := make([]Sandbox, 0)
	partial := false
	token := ""
	for {
		path := "/v1/sandboxes?limit=500"
		if token != "" {
			path += "&page_token=" + url.QueryEscape(token)
		}
		var page []Sandbox
		header, err := c.doWithHeaders(ctx, http.MethodGet, path, nil, &page, true)
		if err != nil {
			return nil, false, err
		}
		all = append(all, page...)
		if strings.EqualFold(header.Get("X-Cluster-List-Partial"), "true") {
			partial = true
		}
		token = header.Get("X-Cluster-List-Next-Page-Token")
		if token == "" {
			return all, partial, nil
		}
	}
}

// GetSandbox calls GET /v1/sandboxes/{id}. A missing sandbox is an *APIError
// with status 404, because callers have to tell it from a failure.
func (c *Client) GetSandbox(ctx context.Context, id string) (Sandbox, error) {
	var out Sandbox
	err := c.do(ctx, http.MethodGet, "/v1/sandboxes/"+url.PathEscape(id), nil, &out, true)
	return out, err
}

// CreateSandbox calls POST /v1/sandboxes.
func (c *Client) CreateSandbox(ctx context.Context, req CreateSandboxRequest) (Sandbox, error) {
	var out Sandbox
	err := c.do(ctx, http.MethodPost, "/v1/sandboxes", req, &out, true)
	return out, err
}

// CreateSnapshot calls POST /v1/sandboxes/{id}/snapshot. A name the team
// already uses is a 409.
func (c *Client) CreateSnapshot(ctx context.Context, sandboxID, name string) (Snapshot, error) {
	var out Snapshot
	body := struct {
		Name string `json:"name"`
	}{Name: name}
	err := c.do(ctx, http.MethodPost, "/v1/sandboxes/"+url.PathEscape(sandboxID)+"/snapshot", body, &out, true)
	return out, err
}

// GetSnapshot calls GET /v1/snapshots/{name}. A missing snapshot is an
// *APIError with status 404.
func (c *Client) GetSnapshot(ctx context.Context, name string) (Snapshot, error) {
	var out Snapshot
	err := c.do(ctx, http.MethodGet, "/v1/snapshots/"+url.PathEscape(name), nil, &out, true)
	return out, err
}

// ListSnapshots reads every page of GET /v1/snapshots. It is node-local: a
// team-bound token sees only that team's snapshots on the node that answers.
func (c *Client) ListSnapshots(ctx context.Context) ([]Snapshot, error) {
	all := make([]Snapshot, 0)
	after := ""
	for {
		path := "/v1/snapshots?limit=500"
		if after != "" {
			path += "&starting_after=" + url.QueryEscape(after)
		}
		var page struct {
			Snapshots []Snapshot `json:"snapshots"`
			HasMore   bool       `json:"has_more"`
		}
		if err := c.do(ctx, http.MethodGet, path, nil, &page, true); err != nil {
			return nil, err
		}
		all = append(all, page.Snapshots...)
		if !page.HasMore || len(page.Snapshots) == 0 {
			return all, nil
		}
		after = page.Snapshots[len(page.Snapshots)-1].Name
	}
}

// DeleteSnapshot calls DELETE /v1/snapshots/{name}. A missing snapshot is a
// 404, because the caller has to find the node that holds it.
func (c *Client) DeleteSnapshot(ctx context.Context, name string) error {
	return c.do(ctx, http.MethodDelete, "/v1/snapshots/"+url.PathEscape(name), nil, nil, true)
}

// SandboxIndex reads every page of GET /v1/cluster/sandbox-index.
func (c *Client) SandboxIndex(ctx context.Context) ([]Placement, error) {
	all := make([]Placement, 0)
	token := ""
	for {
		path := "/v1/cluster/sandbox-index?limit=5000"
		if token != "" {
			path += "&page_token=" + url.QueryEscape(token)
		}
		var page struct {
			Placements    []Placement `json:"placements"`
			NextPageToken string      `json:"next_page_token"`
		}
		if err := c.do(ctx, http.MethodGet, path, nil, &page, true); err != nil {
			return nil, err
		}
		all = append(all, page.Placements...)
		token = page.NextPageToken
		if token == "" {
			return all, nil
		}
	}
}

// PutTeamQuota calls PUT /v1/teams/{team}/quota.
func (c *Client) PutTeamQuota(ctx context.Context, team string, quota TeamQuota) error {
	return c.do(ctx, http.MethodPut, "/v1/teams/"+url.PathEscape(team)+"/quota", quota, nil, true)
}

// DeleteTeamQuota calls DELETE /v1/teams/{team}/quota.
func (c *Client) DeleteTeamQuota(ctx context.Context, team string) error {
	return c.do(ctx, http.MethodDelete, "/v1/teams/"+url.PathEscape(team)+"/quota", nil, nil, true)
}

// StopSandbox calls POST /v1/sandboxes/{id}/stop. A sandbox that is already
// gone is not an error.
func (c *Client) StopSandbox(ctx context.Context, id string) error {
	return c.goneOK(ctx, http.MethodPost, "/v1/sandboxes/"+url.PathEscape(id)+"/stop")
}

// DeleteSandbox calls DELETE /v1/sandboxes/{id}. A sandbox that is already
// gone is not an error.
func (c *Client) DeleteSandbox(ctx context.Context, id string) error {
	return c.goneOK(ctx, http.MethodDelete, "/v1/sandboxes/"+url.PathEscape(id))
}

func (c *Client) goneOK(ctx context.Context, method, path string) error {
	err := c.do(ctx, method, path, nil, nil, true)
	if apiErr, ok := errors.AsType[*APIError](err); ok && apiErr.Status == http.StatusNotFound {
		return nil
	}
	return err
}

// do sends one logical request, retrying the one 503 that usually clears up:
// a token write before raft has a leader.
func (c *Client) do(ctx context.Context, method, path string, body, out any, auth bool) error {
	_, err := c.doWithHeaders(ctx, method, path, body, out, auth)
	return err
}

// doWithHeaders is do, returning the final response's headers. Paged cluster
// routes put their next page token there.
func (c *Client) doWithHeaders(ctx context.Context, method, path string, body, out any, auth bool) (http.Header, error) {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("aerolvm %s %s: encode request: %w", method, path, err)
		}
	}

	var waited time.Duration
	wait := initialRetryWait
	for {
		header, err := c.doOnce(ctx, method, path, payload, out, auth)
		if err == nil {
			return header, nil
		}
		if !retryable(err) || waited+wait > c.retry {
			return nil, err
		}
		if ctx.Err() != nil {
			return nil, err
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

func (c *Client) doOnce(ctx context.Context, method, path string, body []byte, out any, auth bool) (http.Header, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.callTimeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("aerolvm %s %s: %w", method, path, err)
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
		return nil, fmt.Errorf("aerolvm %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("aerolvm %s %s: read response: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, parseAPIError(resp.StatusCode, data)
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return nil, fmt.Errorf("aerolvm %s %s: decode response: %w", method, path, err)
		}
	}
	return resp.Header, nil
}

func retryable(err error) bool {
	apiErr, ok := errors.AsType[*APIError](err)
	if !ok || apiErr.Status != http.StatusServiceUnavailable {
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
