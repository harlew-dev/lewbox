// Package objstore is a small S3-compatible object store client: enough for a
// storage profile's test and for the archive keys of stopped sandboxes. It
// signs every request with AWS Signature Version 4 and uses only the standard
// library.
package objstore

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// defaultTimeout bounds one request, including reading its body.
const defaultTimeout = 30 * time.Second

// emptyPayloadHash is the SHA-256 of an empty body.
const emptyPayloadHash = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// ErrNotFound means the object does not exist.
var ErrNotFound = errors.New("object not found")

// Config names one bucket and the credentials for it. Endpoint is empty for
// AWS S3; Region is required, and "auto" is fine for Cloudflare R2.
type Config struct {
	Endpoint        string // "" = AWS: https://s3.<region>.amazonaws.com
	Region          string // required; "auto" is fine (R2)
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	PathStyle       bool // true: <endpoint>/<bucket>/<key>; false: <bucket>.<endpoint host>/<key>
}

// Object is one key in a bucket listing.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// APIError is a refusal or failure from the object store. It never contains
// the secret access key.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	switch {
	case e.Code != "" && e.Message != "":
		return fmt.Sprintf("S3: HTTP %d: %s: %s", e.Status, e.Code, e.Message)
	case e.Code != "":
		return fmt.Sprintf("S3: HTTP %d: %s", e.Status, e.Code)
	default:
		return fmt.Sprintf("S3: HTTP %d: %s", e.Status, http.StatusText(e.Status))
	}
}

// Client calls one bucket with one key. It is safe for concurrent use.
type Client struct {
	cfg    Config
	scheme string
	host   string
	http   *http.Client
	now    func() time.Time
}

// Option changes a Client built by New.
type Option func(*Client)

// WithHTTPClient replaces the HTTP client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// WithClock replaces the clock the client signs with, such as in tests.
func WithClock(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

// New validates cfg and returns a client for it.
func New(cfg Config, opts ...Option) (*Client, error) {
	cfg.Region = strings.TrimSpace(cfg.Region)
	cfg.Bucket = strings.TrimSpace(cfg.Bucket)
	cfg.AccessKeyID = strings.TrimSpace(cfg.AccessKeyID)
	if cfg.Region == "" {
		return nil, errors.New("objstore: region is required")
	}
	if cfg.Bucket == "" {
		return nil, errors.New("objstore: bucket is required")
	}
	if cfg.AccessKeyID == "" {
		return nil, errors.New("objstore: access key id is required")
	}
	if cfg.SecretAccessKey == "" {
		return nil, errors.New("objstore: secret access key is required")
	}

	c := &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: defaultTimeout},
		now:  func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(c)
	}

	endpoint := strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
	switch {
	case endpoint == "":
		c.scheme = "https"
		c.host = "s3." + cfg.Region + ".amazonaws.com"
	default:
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("objstore: endpoint: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, errors.New("objstore: endpoint must start with http:// or https://")
		}
		if u.Host == "" {
			return nil, errors.New("objstore: endpoint has no host")
		}
		if u.User != nil {
			return nil, errors.New("objstore: endpoint must not carry credentials")
		}
		if u.Path != "" && u.Path != "/" {
			return nil, errors.New("objstore: endpoint must not have a path")
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("objstore: endpoint must not have a query or fragment")
		}
		c.scheme = u.Scheme
		c.host = u.Host
	}
	c.cfg.Endpoint = endpoint
	return c, nil
}

// Put writes body to key, replacing what was there.
func (c *Client) Put(ctx context.Context, key string, body []byte) error {
	resp, err := c.do(ctx, http.MethodPut, key, nil, body, nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// Get reads key. A missing key is ErrNotFound.
func (c *Client) Get(ctx context.Context, key string) ([]byte, error) {
	resp, err := c.do(ctx, http.MethodGet, key, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		io.Copy(io.Discard, resp.Body)
		return nil, ErrNotFound
	}
	if resp.StatusCode/100 != 2 {
		return nil, apiErrorFrom(resp)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", key, err)
	}
	return data, nil
}

// Delete removes key. A missing key is success.
func (c *Client) Delete(ctx context.Context, key string) error {
	resp, err := c.do(ctx, http.MethodDelete, key, nil, nil, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotFound {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return nil
	}
	return checkResponse(resp)
}

// listResult is ListObjectsV2's response.
type listResult struct {
	XMLName  xml.Name `xml:"ListBucketResult"`
	Contents []struct {
		Key          string    `xml:"Key"`
		Size         int64     `xml:"Size"`
		LastModified time.Time `xml:"LastModified"`
	} `xml:"Contents"`
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
}

// List returns up to max objects under prefix, and the continuation token for
// the next page ("" when there is none). max <= 0 asks for the store's
// default.
func (c *Client) List(ctx context.Context, prefix, continuation string, max int) ([]Object, string, error) {
	query := url.Values{"list-type": {"2"}}
	if prefix != "" {
		query.Set("prefix", prefix)
	}
	if continuation != "" {
		query.Set("continuation-token", continuation)
	}
	if max > 0 {
		query.Set("max-keys", strconv.Itoa(max))
	}
	resp, err := c.do(ctx, http.MethodGet, "", query, nil, nil)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, "", apiErrorFrom(resp)
	}
	var out listResult
	if err := xml.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, "", fmt.Errorf("list %s: %w", prefix, err)
	}
	objs := make([]Object, 0, len(out.Contents))
	for _, item := range out.Contents {
		objs = append(objs, Object{Key: item.Key, Size: item.Size, LastModified: item.LastModified.UTC()})
	}
	next := ""
	if out.IsTruncated {
		next = out.NextContinuationToken
	}
	return objs, next, nil
}

// DeletePrefix removes every object under prefix in batches of up to 1000 and
// returns how many it removed.
func (c *Client) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	var keys []string
	continuation := ""
	for {
		objs, next, err := c.List(ctx, prefix, continuation, 1000)
		if err != nil {
			return 0, err
		}
		for _, obj := range objs {
			keys = append(keys, obj.Key)
		}
		if next == "" {
			break
		}
		continuation = next
	}

	removed := 0
	for len(keys) > 0 {
		n := min(1000, len(keys))
		batch := append([]string(nil), keys[:n]...)
		keys = keys[n:]
		if err := c.deleteObjects(ctx, batch); err != nil {
			return removed, err
		}
		removed += len(batch)
	}
	return removed, nil
}

// deleteRequest is DeleteObjects' body.
type deleteRequest struct {
	XMLName xml.Name       `xml:"Delete"`
	Quiet   bool           `xml:"Quiet"`
	Objects []deleteObject `xml:"Object"`
}

type deleteObject struct {
	Key string `xml:"Key"`
}

// deleteResultError is one failed key in DeleteObjects' response.
type deleteResultError struct {
	Key     string `xml:"Key"`
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

type deleteResult struct {
	XMLName xml.Name            `xml:"DeleteResult"`
	Errors  []deleteResultError `xml:"Error"`
}

// deleteObjects deletes one batch with POST ?delete.
func (c *Client) deleteObjects(ctx context.Context, keys []string) error {
	req := deleteRequest{Objects: make([]deleteObject, 0, len(keys))}
	for _, key := range keys {
		req.Objects = append(req.Objects, deleteObject{Key: key})
	}
	body, err := xml.Marshal(req)
	if err != nil {
		return fmt.Errorf("delete objects: %w", err)
	}
	sum := md5.Sum(body)
	headers := http.Header{
		"Content-Type": {"application/xml"},
		"Content-Md5":  {base64.StdEncoding.EncodeToString(sum[:])},
	}
	resp, err := c.do(ctx, http.MethodPost, "", url.Values{"delete": {""}}, body, headers)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return apiErrorFrom(resp)
	}
	var out deleteResult
	if err := xml.NewDecoder(resp.Body).Decode(&out); err != nil && err != io.EOF {
		return fmt.Errorf("delete objects: %w", err)
	}
	if len(out.Errors) > 0 {
		first := out.Errors[0]
		return &APIError{Status: resp.StatusCode, Code: first.Code, Message: first.Message}
	}
	return nil
}

// do signs and sends one request. key is empty for bucket-level calls, where
// query selects the operation.
func (c *Client) do(ctx context.Context, method, key string, query url.Values, body []byte, headers http.Header) (*http.Response, error) {
	u, err := c.url(key, query)
	if err != nil {
		return nil, err
	}
	if headers == nil {
		headers = make(http.Header)
	}
	payloadHash := emptyPayloadHash
	var reader io.Reader
	if len(body) > 0 {
		sum := sha256.Sum256(body)
		payloadHash = hex.EncodeToString(sum[:])
		reader = bytes.NewReader(body)
	}
	c.sign(method, u, headers, payloadHash, c.now())

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header = headers
	return c.http.Do(req)
}

// url builds the request URL. The path is encoded per AWS rules, so a key's
// slashes stay and every other reserved character is escaped.
func (c *Client) url(key string, query url.Values) (*url.URL, error) {
	scheme, host := c.scheme, c.host
	var path string
	if c.cfg.PathStyle {
		path = "/" + uriEncode(c.cfg.Bucket)
		if key != "" {
			path += "/" + encodeKey(key)
		}
	} else {
		host = c.cfg.Bucket + "." + host
		if key != "" {
			path = "/" + encodeKey(key)
		}
	}
	if path == "" {
		path = "/"
	}
	raw := scheme + "://" + host + path
	if q := encodeQuery(query); q != "" {
		raw += "?" + q
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("build URL: %w", err)
	}
	return u, nil
}

// sign adds the SigV4 headers for a request to u with the given payload hash.
// It signs every header already in headers, plus host.
func (c *Client) sign(method string, u *url.URL, headers http.Header, payloadHash string, now time.Time) {
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	headers.Set("x-amz-date", amzDate)
	headers.Set("x-amz-content-sha256", payloadHash)

	canonicalURI := u.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQuery := encodeQuery(u.Query())

	type header struct{ name, value string }
	var list []header
	list = append(list, header{"host", u.Host})
	for name, values := range headers {
		lower := strings.ToLower(name)
		if lower == "authorization" || lower == "host" || lower == "user-agent" {
			continue
		}
		list = append(list, header{lower, canonicalHeaderValue(strings.Join(values, ","))})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].name < list[j].name })

	var canonicalHeaders strings.Builder
	names := make([]string, 0, len(list))
	for _, h := range list {
		canonicalHeaders.WriteString(h.name)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(h.value)
		canonicalHeaders.WriteByte('\n')
		names = append(names, h.name)
	}
	signedHeaders := strings.Join(names, ";")

	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := date + "/" + c.cfg.Region + "/s3/aws4_request"
	requestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hex.EncodeToString(requestHash[:]),
	}, "\n")

	signature := hex.EncodeToString(hmacSHA256(c.signingKey(date), []byte(stringToSign)))
	headers.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+c.cfg.AccessKeyID+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}

// signingKey derives the SigV4 signing key for a date (YYYYMMDD).
func (c *Client) signingKey(date string) []byte {
	key := hmacSHA256([]byte("AWS4"+c.cfg.SecretAccessKey), []byte(date))
	key = hmacSHA256(key, []byte(c.cfg.Region))
	key = hmacSHA256(key, []byte("s3"))
	return hmacSHA256(key, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// checkResponse closes resp and returns its error for a failed status.
func checkResponse(resp *http.Response) error {
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return apiErrorFrom(resp)
}

// errorResponse is S3's XML error shape.
type errorResponse struct {
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

// apiErrorFrom turns an S3 error response into an APIError.
func apiErrorFrom(resp *http.Response) error {
	defer resp.Body.Close()
	out := APIError{Status: resp.StatusCode, Message: http.StatusText(resp.StatusCode)}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err == nil && len(body) > 0 {
		var detail errorResponse
		if xml.Unmarshal(body, &detail) == nil {
			if detail.Code != "" {
				out.Code = detail.Code
			}
			if detail.Message != "" {
				out.Message = detail.Message
			}
		}
	}
	return &out
}

// uriEncode percent-encodes one path segment or query value per AWS rules:
// only A-Z, a-z, 0-9, -, _, . and ~ stay as they are.
func uriEncode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9',
			ch == '-', ch == '_', ch == '.', ch == '~':
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

// encodeKey encodes each segment of an object key, keeping its slashes.
func encodeKey(key string) string {
	segments := strings.Split(key, "/")
	for i := range segments {
		segments[i] = uriEncode(segments[i])
	}
	return strings.Join(segments, "/")
}

// encodeQuery encodes and sorts a query string per AWS rules.
func encodeQuery(query url.Values) string {
	if len(query) == 0 {
		return ""
	}
	names := make([]string, 0, len(query))
	for name := range query {
		names = append(names, name)
	}
	sort.Strings(names)
	var parts []string
	for _, name := range names {
		values := append([]string(nil), query[name]...)
		sort.Strings(values)
		if len(values) == 0 {
			parts = append(parts, uriEncode(name)+"=")
			continue
		}
		for _, value := range values {
			parts = append(parts, uriEncode(name)+"="+uriEncode(value))
		}
	}
	return strings.Join(parts, "&")
}

// canonicalHeaderValue trims a header value and collapses inner whitespace, as
// SigV4 requires.
func canonicalHeaderValue(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
