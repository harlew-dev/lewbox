package cloudflare

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFindZone(t *testing.T) {
	var gotName, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotName = r.URL.Query().Get("name")
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/zones" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success":true,"errors":[],"result":[
			{"id":"z1","name":"Example.com.","status":"active"},
			{"id":"z2","name":"other.com","status":"active"}
		]}`))
	}))
	defer srv.Close()

	zone, err := New("secret-token", WithBaseURL(srv.URL)).FindZone(context.Background(), "example.com.")
	if err != nil {
		t.Fatal(err)
	}
	if zone.ID != "z1" || zone.Status != "active" {
		t.Errorf("zone = %+v", zone)
	}
	if gotName != "example.com" {
		t.Errorf("query name = %q", gotName)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("authorization = %q", gotAuth)
	}
}

func TestFindZoneNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"success":true,"errors":[],"result":[{"id":"z2","name":"other.com","status":"active"}]}`))
	}))
	defer srv.Close()

	_, err := New("t", WithBaseURL(srv.URL)).FindZone(context.Background(), "example.com")
	if !errors.Is(err, ErrZoneNotFound) {
		t.Fatalf("error = %v, want ErrZoneNotFound", err)
	}
}

func TestListRecordsFollowsPages(t *testing.T) {
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones/z1/dns_records" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("type") != "TXT" || r.URL.Query().Get("name") != "_x.example.com" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		switch page {
		case "1":
			w.Write([]byte(`{"success":true,"errors":[],"result":[{"id":"r1","type":"TXT","name":"_x.example.com","content":"a","ttl":60}],
				"result_info":{"page":1,"per_page":100,"total_pages":2}}`))
		case "2":
			w.Write([]byte(`{"success":true,"errors":[],"result":[{"id":"r2","type":"TXT","name":"_x.example.com","content":"b","ttl":60,"proxied":true}],
				"result_info":{"page":2,"per_page":100,"total_pages":2}}`))
		default:
			t.Errorf("unexpected page %q", page)
		}
	}))
	defer srv.Close()

	records, err := New("t", WithBaseURL(srv.URL)).ListRecords(context.Background(), "z1", "TXT", "_x.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].ID != "r1" || records[1].ID != "r2" || !records[1].Proxied {
		t.Errorf("records = %+v", records)
	}
	if len(pages) != 2 {
		t.Errorf("pages = %v", pages)
	}
}

func TestCreateAndDeleteRecord(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		if r.Method == http.MethodPost {
			data, _ := io.ReadAll(r.Body)
			gotBody = string(data)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"success":true,"errors":[],"result":{"id":"r9","type":"TXT","name":"_t.example.com","content":"abc","ttl":60}}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"success":false,"errors":[{"code":81044,"message":"Record does not exist."}]}`))
	}))
	defer srv.Close()

	client := New("t", WithBaseURL(srv.URL))
	record, err := client.CreateRecord(context.Background(), "z1", Record{
		Type: "TXT", Name: "_t.example.com", Content: "abc", TTL: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != "r9" || record.TTL != 60 {
		t.Errorf("record = %+v", record)
	}
	if gotMethod != http.MethodPost || gotPath != "/zones/z1/dns_records" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(gotBody, `"type":"TXT"`) || !strings.Contains(gotBody, `"content":"abc"`) {
		t.Errorf("body = %s", gotBody)
	}

	// A delete of a record that is already gone is fine.
	if err := client.DeleteRecord(context.Background(), "z1", "r9"); err != nil {
		t.Fatalf("delete missing record: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/zones/z1/dns_records/r9" {
		t.Errorf("request = %s %s", gotMethod, gotPath)
	}
}

func TestAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`))
	}))
	defer srv.Close()

	_, err := New("super-secret", WithBaseURL(srv.URL)).FindZone(context.Background(), "example.com")
	apiErr, ok := errors.AsType[*APIError](err)
	if !ok {
		t.Fatalf("error = %v", err)
	}
	if apiErr.Status != 403 || apiErr.Code != 10000 || apiErr.Message != "Authentication error" {
		t.Errorf("api error = %+v", apiErr)
	}
	want := "cloudflare: HTTP 403: Authentication error (code 10000)"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Errorf("the error carries the token: %v", err)
	}
}

func TestDefaultBaseURL(t *testing.T) {
	c := New("t", WithBaseURL(""))
	if c.baseURL != DefaultBaseURL {
		t.Errorf("base URL = %q", c.baseURL)
	}
	c = New("t", WithBaseURL("https://example.com/api/"))
	if c.baseURL != "https://example.com/api" {
		t.Errorf("base URL = %q", c.baseURL)
	}
}
