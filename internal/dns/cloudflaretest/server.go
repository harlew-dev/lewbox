// Package cloudflaretest provides an in-memory fake Cloudflare API for tests.
package cloudflaretest

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/harlew-dev/lewbox/internal/dns/cloudflare"
)

// Fake is a fake Cloudflare API backed by an httptest.Server.
type Fake struct {
	t   testing.TB
	srv *httptest.Server

	// URL is the base URL to pass to cloudflare.WithBaseURL.
	URL string
	// Token is the only bearer token the fake accepts.
	Token string

	mu       sync.Mutex
	zones    map[string]cloudflare.Zone
	zoneIDs  []string
	records  map[string][]cloudflare.Record
	failures []*failure
	nextID   int
}

type failure struct {
	method string
	prefix string
	status int
	n      int
}

// cloudflareError is one entry of a response's errors list.
type cloudflareError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// envelope is Cloudflare's response shape.
type envelope struct {
	Success    bool              `json:"success"`
	Errors     []cloudflareError `json:"errors"`
	Result     any               `json:"result"`
	ResultInfo *resultInfo       `json:"result_info,omitempty"`
}

type resultInfo struct {
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Count      int `json:"count"`
	TotalCount int `json:"total_count"`
	TotalPages int `json:"total_pages"`
}

// New starts a fake API and closes it when the test finishes.
func New(t testing.TB) *Fake {
	t.Helper()
	var tokenBytes [18]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		t.Fatal(err)
	}
	f := &Fake{
		t:       t,
		Token:   hex.EncodeToString(tokenBytes[:]),
		zones:   make(map[string]cloudflare.Zone),
		records: make(map[string][]cloudflare.Record),
	}
	f.srv = httptest.NewServer(f.routes())
	f.URL = f.srv.URL
	t.Cleanup(f.srv.Close)
	return f
}

// Close stops the fake early. The test's cleanup would stop it anyway.
func (f *Fake) Close() {
	f.srv.Close()
}

// AddZone adds a zone and returns its ID.
func (f *Fake) AddZone(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := fmt.Sprintf("zone-%d", f.nextID)
	f.zones[id] = cloudflare.Zone{ID: id, Name: name, Status: "active"}
	f.zoneIDs = append(f.zoneIDs, id)
	return id
}

// Records returns a copy of a zone's records, sorted by name and content.
func (f *Fake) Records(zoneID string) []cloudflare.Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]cloudflare.Record(nil), f.records[zoneID]...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Content < out[j].Content
	})
	return out
}

// FailNext makes the next n requests whose method matches and whose path
// starts with prefix answer status. Every matching request counts, whichever
// endpoint handles it.
func (f *Fake) FailNext(method, pathPrefix string, status, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failures = append(f.failures, &failure{method: method, prefix: pathPrefix, status: status, n: n})
}

func (f *Fake) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /zones", f.findZones)
	mux.HandleFunc("GET /zones/{zone}/dns_records", f.listRecords)
	mux.HandleFunc("POST /zones/{zone}/dns_records", f.createRecord)
	mux.HandleFunc("DELETE /zones/{zone}/dns_records/{record}", f.deleteRecord)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status, failed := f.takeFailure(r); failed {
			writeError(w, status, 10000, "injected failure")
			return
		}
		if !f.authorized(r) {
			if r.Header.Get("Authorization") == "" {
				writeError(w, http.StatusUnauthorized, 10000, "Authentication error")
			} else {
				writeError(w, http.StatusForbidden, 10000, "Authentication error")
			}
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// takeFailure consumes one pending failure that matches the request.
func (f *Fake) takeFailure(r *http.Request) (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, fail := range f.failures {
		if fail.n > 0 && fail.method == r.Method && strings.HasPrefix(r.URL.Path, fail.prefix) {
			fail.n--
			return fail.status, true
		}
	}
	return 0, false
}

func (f *Fake) authorized(r *http.Request) bool {
	return r.Header.Get("Authorization") == "Bearer "+f.Token
}

func (f *Fake) findZones(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(strings.ToLower(r.URL.Query().Get("name")), ".")
	f.mu.Lock()
	zoneIDs := append([]string(nil), f.zoneIDs...)
	f.mu.Unlock()

	zones := make([]cloudflare.Zone, 0, len(zoneIDs))
	for _, id := range zoneIDs {
		f.mu.Lock()
		zone := f.zones[id]
		f.mu.Unlock()
		if name == "" || strings.TrimSuffix(strings.ToLower(zone.Name), ".") == name {
			zones = append(zones, zone)
		}
	}
	writeOK(w, zones, nil)
}

func (f *Fake) listRecords(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	zoneID := r.PathValue("zone")
	if _, ok := f.zones[zoneID]; !ok {
		writeError(w, http.StatusNotFound, 7003, "Could not route to zones, perhaps your object identifier is invalid")
		return
	}
	typ, name := r.URL.Query().Get("type"), r.URL.Query().Get("name")
	matched := make([]cloudflare.Record, 0, len(f.records[zoneID]))
	for _, record := range f.records[zoneID] {
		if typ != "" && record.Type != typ {
			continue
		}
		if name != "" && record.Name != name {
			continue
		}
		matched = append(matched, record)
	}
	perPage := 100
	if raw := r.URL.Query().Get("per_page"); raw != "" {
		fmt.Sscanf(raw, "%d", &perPage)
	}
	if perPage < 1 {
		perPage = 100
	}
	page := 1
	if raw := r.URL.Query().Get("page"); raw != "" {
		fmt.Sscanf(raw, "%d", &page)
	}
	if page < 1 {
		page = 1
	}
	totalPages := (len(matched) + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	start := (page - 1) * perPage
	if start > len(matched) {
		start = len(matched)
	}
	end := start + perPage
	if end > len(matched) {
		end = len(matched)
	}
	writeOK(w, matched[start:end], &resultInfo{
		Page: page, PerPage: perPage, Count: end - start,
		TotalCount: len(matched), TotalPages: totalPages,
	})
}

func (f *Fake) createRecord(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	zoneID := r.PathValue("zone")
	if _, ok := f.zones[zoneID]; !ok {
		writeError(w, http.StatusNotFound, 7003, "Could not route to zones, perhaps your object identifier is invalid")
		return
	}
	var in cloudflare.Record
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, 9000, "Invalid JSON body")
		return
	}
	f.nextID++
	in.ID = fmt.Sprintf("record-%d", f.nextID)
	f.records[zoneID] = append(f.records[zoneID], in)
	writeOK(w, in, nil)
}

func (f *Fake) deleteRecord(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	zoneID := r.PathValue("zone")
	if _, ok := f.zones[zoneID]; !ok {
		writeError(w, http.StatusNotFound, 7003, "Could not route to zones, perhaps your object identifier is invalid")
		return
	}
	id := r.PathValue("record")
	records := f.records[zoneID]
	for i, record := range records {
		if record.ID == id {
			f.records[zoneID] = append(records[:i], records[i+1:]...)
			writeOK(w, cloudflare.Record{ID: id}, nil)
			return
		}
	}
	writeError(w, http.StatusNotFound, 81044, "Record does not exist.")
}

func writeOK(w http.ResponseWriter, result any, info *resultInfo) {
	writeJSON(w, http.StatusOK, envelope{Success: true, Errors: []cloudflareError{}, Result: result, ResultInfo: info})
}

func writeError(w http.ResponseWriter, status, code int, message string) {
	writeJSON(w, status, envelope{
		Success: false,
		Errors:  []cloudflareError{{Code: code, Message: message}},
		Result:  nil,
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
