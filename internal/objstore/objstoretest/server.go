// Package objstoretest provides an in-memory S3-compatible server for tests:
// path-style only, with a few buckets and objects.
package objstoretest

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Fake is a fake S3 server backed by an httptest.Server.
type Fake struct {
	srv *httptest.Server

	// URL is the endpoint to pass to objstore.Config, with PathStyle true.
	URL string
	// AccessKeyID and SecretAccessKey are the credentials the fake accepts.
	AccessKeyID     string
	SecretAccessKey string

	mu       sync.Mutex
	buckets  map[string]map[string][]byte
	failures []*failure
}

type failure struct {
	method string
	prefix string
	status int
	code   string
	n      int
}

// New starts a fake S3 server and closes it when the test finishes.
func New(t testing.TB) *Fake {
	t.Helper()
	var key, secret [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(secret[:]); err != nil {
		t.Fatal(err)
	}
	f := &Fake{
		AccessKeyID:     "lewbox-" + hex.EncodeToString(key[:8]),
		SecretAccessKey: hex.EncodeToString(secret[:]),
		buckets:         make(map[string]map[string][]byte),
	}
	f.srv = httptest.NewServer(f.routes())
	f.URL = f.srv.URL
	t.Cleanup(f.srv.Close)
	return f
}

// AddBucket creates an empty bucket.
func (f *Fake) AddBucket(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.buckets[name]; !ok {
		f.buckets[name] = make(map[string][]byte)
	}
}

// Objects returns a copy of a bucket's objects, keyed by key.
func (f *Fake) Objects(bucket string) map[string][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string][]byte, len(f.buckets[bucket]))
	for key, body := range f.buckets[bucket] {
		out[key] = append([]byte(nil), body...)
	}
	return out
}

// FailNext makes the next n requests whose method matches and whose object key
// starts with prefix answer status with an S3 error code. Every matching
// request counts, whichever operation handles it.
func (f *Fake) FailNext(method, keyPrefix string, status int, code string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures = append(f.failures, &failure{
		method: strings.ToUpper(method),
		prefix: keyPrefix,
		status: status,
		code:   code,
		n:      n,
	})
}

// fail reports whether this request is one the test asked to fail.
func (f *Fake) fail(method, key string) *failure {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, item := range f.failures {
		if item.n <= 0 || item.method != method || !strings.HasPrefix(key, item.prefix) {
			continue
		}
		item.n--
		return item
	}
	return nil
}

func (f *Fake) routes() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bucket, key := splitPath(r.URL.Path)
		if !f.authorized(r) {
			writeError(w, http.StatusForbidden, "InvalidAccessKeyId", "The AWS Access Key Id you provided does not exist in our records.")
			return
		}
		if item := f.fail(r.Method, key); item != nil {
			writeError(w, item.status, item.code, "injected failure")
			return
		}

		f.mu.Lock()
		objects, known := f.buckets[bucket]
		f.mu.Unlock()
		if !known {
			writeError(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist")
			return
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
			f.list(w, r, bucket, objects)
		case r.Method == http.MethodGet:
			f.get(w, key, objects)
		case r.Method == http.MethodPut:
			f.put(w, r, bucket, key)
		case r.Method == http.MethodDelete:
			f.delete(w, bucket, key)
		case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
			f.deleteBatch(w, r, bucket)
		default:
			writeError(w, http.StatusBadRequest, "InvalidRequest", "unsupported request")
		}
	})
}

// authorized checks that the Authorization header names the fake's access key.
// It does not verify the signature.
func (f *Fake) authorized(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	return strings.Contains(auth, "Credential="+f.AccessKeyID+"/")
}

func (f *Fake) get(w http.ResponseWriter, key string, objects map[string][]byte) {
	f.mu.Lock()
	body, ok := objects[key]
	body = append([]byte(nil), body...)
	f.mu.Unlock()
	if !ok {
		writeError(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func (f *Fake) put(w http.ResponseWriter, r *http.Request, bucket, key string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "InvalidRequest", err.Error())
		return
	}
	f.mu.Lock()
	objects := f.buckets[bucket]
	if objects == nil {
		objects = make(map[string][]byte)
		f.buckets[bucket] = objects
	}
	objects[key] = body
	f.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (f *Fake) delete(w http.ResponseWriter, bucket, key string) {
	f.mu.Lock()
	delete(f.buckets[bucket], key)
	f.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

type listObject struct {
	Key          string `xml:"Key"`
	Size         int64  `xml:"Size"`
	LastModified string `xml:"LastModified"`
}

type listBucketResult struct {
	XMLName               xml.Name     `xml:"ListBucketResult"`
	Name                  string       `xml:"Name"`
	Prefix                string       `xml:"Prefix"`
	KeyCount              int          `xml:"KeyCount"`
	MaxKeys               int          `xml:"MaxKeys"`
	IsTruncated           bool         `xml:"IsTruncated"`
	NextContinuationToken string       `xml:"NextContinuationToken,omitempty"`
	Contents              []listObject `xml:"Contents"`
}

func (f *Fake) list(w http.ResponseWriter, r *http.Request, bucket string, objects map[string][]byte) {
	query := r.URL.Query()
	prefix := query.Get("prefix")
	after := query.Get("continuation-token")
	maxKeys := 1000
	if raw := query.Get("max-keys"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			maxKeys = n
		}
	}

	f.mu.Lock()
	keys := make([]string, 0, len(objects))
	for key := range objects {
		if strings.HasPrefix(key, prefix) && key > after {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	f.mu.Unlock()

	truncated := len(keys) > maxKeys
	if truncated {
		keys = keys[:maxKeys]
	}
	out := listBucketResult{
		Name:        bucket,
		Prefix:      prefix,
		KeyCount:    len(keys),
		MaxKeys:     maxKeys,
		IsTruncated: truncated,
	}
	for _, key := range keys {
		f.mu.Lock()
		size := int64(len(objects[key]))
		f.mu.Unlock()
		out.Contents = append(out.Contents, listObject{
			Key:          key,
			Size:         size,
			LastModified: time.Now().UTC().Format(time.RFC3339),
		})
	}
	if truncated && len(keys) > 0 {
		out.NextContinuationToken = keys[len(keys)-1]
	}
	writeXML(w, http.StatusOK, out)
}

type deleteRequest struct {
	Objects []struct {
		Key string `xml:"Key"`
	} `xml:"Object"`
}

type deletedObject struct {
	Key string `xml:"Key"`
}

type deleteResult struct {
	XMLName xml.Name        `xml:"DeleteResult"`
	Deleted []deletedObject `xml:"Deleted"`
}

func (f *Fake) deleteBatch(w http.ResponseWriter, r *http.Request, bucket string) {
	if r.Header.Get("Content-Md5") == "" {
		writeError(w, http.StatusBadRequest, "InvalidRequest", "Content-MD5 is required")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "InvalidRequest", err.Error())
		return
	}
	var req deleteRequest
	if err := xml.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "MalformedXML", err.Error())
		return
	}
	out := deleteResult{}
	f.mu.Lock()
	for _, obj := range req.Objects {
		delete(f.buckets[bucket], obj.Key)
		out.Deleted = append(out.Deleted, deletedObject{Key: obj.Key})
	}
	f.mu.Unlock()
	writeXML(w, http.StatusOK, out)
}

// splitPath splits a path-style request into bucket and key.
func splitPath(path string) (bucket, key string) {
	rest := strings.TrimPrefix(path, "/")
	unescaped, err := url.PathUnescape(rest)
	if err == nil {
		rest = unescaped
	}
	bucket, key, _ = strings.Cut(rest, "/")
	return bucket, key
}

// writeError writes S3's XML error shape.
func writeError(w http.ResponseWriter, status int, code, message string) {
	type errorBody struct {
		XMLName xml.Name `xml:"Error"`
		Code    string   `xml:"Code"`
		Message string   `xml:"Message"`
	}
	writeXML(w, status, errorBody{Code: code, Message: message})
}

func writeXML(w http.ResponseWriter, status int, v any) {
	data, err := xml.Marshal(v)
	if err != nil {
		http.Error(w, fmt.Sprintf("marshal: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	w.Write([]byte(xml.Header))
	w.Write(data)
}
