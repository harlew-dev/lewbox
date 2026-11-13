// Package cloudflare is a small client for the Cloudflare API: zones and DNS
// records, which is what a Lewbox DNS profile tests and then uses to point a
// cluster's domain at its nodes.
package cloudflare

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

// DefaultBaseURL is Cloudflare's API version 4 base.
const DefaultBaseURL = "https://api.cloudflare.com/client/v4"

// ErrZoneNotFound means the token cannot see a zone with the requested name.
var ErrZoneNotFound = errors.New("zone not found")

// Zone is one Cloudflare zone.
type Zone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Record is one DNS record in a zone.
type Record struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
	Comment string `json:"comment,omitempty"`
}

// APIError is a refusal or failure from Cloudflare. The token is never part
// of it.
type APIError struct {
	Status  int
	Code    int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("cloudflare: HTTP %d: %s (code %d)", e.Status, e.Message, e.Code)
}

// Client calls the Cloudflare API with one API token.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// Option changes a Client built by New.
type Option func(*Client)

// WithBaseURL points the client at another API base, such as a test server.
// An empty u keeps the default.
func WithBaseURL(u string) Option {
	return func(c *Client) {
		if u != "" {
			c.baseURL = strings.TrimRight(u, "/")
		}
	}
}

// WithHTTPClient replaces the HTTP client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// New returns a client for the Cloudflare API using token.
func New(token string, opts ...Option) *Client {
	c := &Client{
		token:   token,
		baseURL: DefaultBaseURL,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// FindZone returns the zone named name. The name is compared exactly, in
// lower case and without a trailing dot. A zone the token cannot see is
// ErrZoneNotFound.
func (c *Client) FindZone(ctx context.Context, name string) (Zone, error) {
	name = normalizeZone(name)
	var zones []Zone
	if _, err := c.do(ctx, http.MethodGet, "/zones", url.Values{"name": {name}}, nil, &zones); err != nil {
		return Zone{}, err
	}
	for _, zone := range zones {
		if normalizeZone(zone.Name) == name {
			return zone, nil
		}
	}
	return Zone{}, ErrZoneNotFound
}

// ListRecords returns a zone's DNS records, optionally filtered by type and
// name. Every page is followed.
func (c *Client) ListRecords(ctx context.Context, zoneID, typ, name string) ([]Record, error) {
	var records []Record
	for page := 1; ; page++ {
		query := url.Values{}
		query.Set("per_page", "100")
		query.Set("page", fmt.Sprintf("%d", page))
		if typ != "" {
			query.Set("type", typ)
		}
		if name != "" {
			query.Set("name", name)
		}
		var pageRecords []Record
		info, err := c.do(ctx, http.MethodGet, "/zones/"+url.PathEscape(zoneID)+"/dns_records", query, nil, &pageRecords)
		if err != nil {
			return nil, err
		}
		records = append(records, pageRecords...)
		if page >= info.TotalPages {
			return records, nil
		}
	}
}

// CreateRecord adds a DNS record to a zone.
func (c *Client) CreateRecord(ctx context.Context, zoneID string, r Record) (Record, error) {
	body := struct {
		Type    string `json:"type"`
		Name    string `json:"name"`
		Content string `json:"content"`
		TTL     int    `json:"ttl"`
		Proxied bool   `json:"proxied"`
		Comment string `json:"comment,omitempty"`
	}{r.Type, r.Name, r.Content, r.TTL, r.Proxied, r.Comment}
	var out Record
	if _, err := c.do(ctx, http.MethodPost, "/zones/"+url.PathEscape(zoneID)+"/dns_records", nil, body, &out); err != nil {
		return Record{}, err
	}
	return out, nil
}

// DeleteRecord removes a DNS record. A record that is already gone (HTTP 404)
// is not an error.
func (c *Client) DeleteRecord(ctx context.Context, zoneID, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/zones/"+url.PathEscape(zoneID)+"/dns_records/"+url.PathEscape(id), nil, nil, nil)
	if apiErr, ok := errors.AsType[*APIError](err); ok && apiErr.Status == http.StatusNotFound {
		return nil
	}
	return err
}

func normalizeZone(name string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(name)), ".")
}

// apiErrorDetail is one entry of a response's errors list.
type apiErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// pageInfo is the pagination part of a list response.
type pageInfo struct {
	TotalPages int `json:"total_pages"`
}

// apiEnvelope is Cloudflare's response shape.
type apiEnvelope struct {
	Success    bool             `json:"success"`
	Errors     []apiErrorDetail `json:"errors"`
	Result     json.RawMessage  `json:"result"`
	ResultInfo *pageInfo        `json:"result_info"`
}

// do makes one API call. It never puts the token in an error. The returned
// page info is zero for calls that have none.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, in, out any) (pageInfo, error) {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return pageInfo{}, fmt.Errorf("encode %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return pageInfo{}, fmt.Errorf("build %s %s: %w", method, path, err)
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	res, err := c.http.Do(req)
	if err != nil {
		return pageInfo{}, fmt.Errorf("cloudflare: %s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return pageInfo{}, fmt.Errorf("cloudflare: read %s %s: %w", method, path, err)
	}

	var envelope apiEnvelope
	_ = json.Unmarshal(data, &envelope)
	if res.StatusCode < 200 || res.StatusCode > 299 || (!envelope.Success && len(envelope.Errors) > 0) {
		return pageInfo{}, apiErrorFrom(res.StatusCode, envelope.Errors)
	}
	if out == nil {
		return pageInfo{}, nil
	}
	if len(envelope.Result) == 0 {
		return pageInfo{}, fmt.Errorf("cloudflare: %s %s: empty result", method, path)
	}
	if err := json.Unmarshal(envelope.Result, out); err != nil {
		return pageInfo{}, fmt.Errorf("cloudflare: decode %s %s: %w", method, path, err)
	}
	if envelope.ResultInfo != nil {
		return *envelope.ResultInfo, nil
	}
	return pageInfo{}, nil
}

// apiErrorFrom turns Cloudflare's errors list into an APIError.
func apiErrorFrom(status int, details []apiErrorDetail) error {
	apiErr := &APIError{Status: status, Message: http.StatusText(status)}
	if len(details) > 0 {
		apiErr.Code = details[0].Code
		if details[0].Message != "" {
			apiErr.Message = details[0].Message
		}
	}
	return apiErr
}
