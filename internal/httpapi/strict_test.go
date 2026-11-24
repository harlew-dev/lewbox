package httpapi_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// bearer logs in and returns the access token.
func (ts *testServer) bearer(t *testing.T) string {
	t.Helper()
	res, body := ts.do("POST", "/v0/auth/login", ts.loginJSON(), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login: status %d body %s", res.StatusCode, body)
	}
	var login struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(body), &login); err != nil {
		t.Fatal(err)
	}
	return login.Token
}

func TestRequestIDOnEveryResponse(t *testing.T) {
	ts := newTestServer(t)
	res, _ := ts.do("GET", "/v0/health", "", nil)
	first := res.Header.Get("X-Request-ID")
	if !regexp.MustCompile(`^req_[0-9A-Za-z]{14}$`).MatchString(first) {
		t.Fatalf("X-Request-ID = %q", first)
	}
	res, _ = ts.do("GET", "/v0/health", "", map[string]string{"X-Request-ID": "req_clientchosen00"})
	second := res.Header.Get("X-Request-ID")
	if second == "req_clientchosen00" {
		t.Error("the server trusted the client's request ID")
	}
	if second == first {
		t.Error("two requests shared a request ID")
	}
}

func TestMethodNotAllowedAndUnknownRoute(t *testing.T) {
	ts := newTestServer(t)
	res, body := ts.do("POST", "/v0/health", "", nil)
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}
	if allow := res.Header.Get("Allow"); !strings.Contains(allow, "GET") {
		t.Errorf("Allow = %q", allow)
	}
	if !strings.Contains(body, `"code":"method_not_allowed"`) {
		t.Errorf("body = %s", body)
	}

	res, body = ts.do("GET", "/v0/nope", "", nil)
	if res.StatusCode != http.StatusNotFound || !strings.Contains(body, `"code":"not_found"`) {
		t.Errorf("unknown route: status %d body %s", res.StatusCode, body)
	}
}

func TestStrictJSONBody(t *testing.T) {
	ts := newTestServer(t)
	token := ts.bearer(t)
	headers := map[string]string{"Authorization": "Bearer " + token}

	cases := []struct {
		name string
		body string
		text string
	}{
		{"malformed", `{"name":`, "not valid JSON"},
		{"trailing value", `{"name":"b","url":"https://b.example"}{"x":1}`, "exactly one JSON object"},
		{"array", `[{"name":"b"}]`, "one JSON object"},
		{"null", `null`, "one JSON object"},
		{"unknown field", `{"name":"b","url":"https://b.example","region":"eu"}`, "unknown field"},
		{"wrong type", `{"name":5,"url":"https://b.example"}`, "wrong type"},
		{"empty", ``, "required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, body := ts.do("POST", "/v0/nodes", tc.body, headers)
			if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, `"code":"bad_request"`) {
				t.Fatalf("status %d body %s", res.StatusCode, body)
			}
			if !strings.Contains(body, tc.text) {
				t.Errorf("body %s does not say %q", body, tc.text)
			}
			if strings.Contains(body, "json:") || strings.Contains(body, "invalid character") {
				t.Errorf("body echoes parser text: %s", body)
			}
		})
	}

	t.Run("oversized", func(t *testing.T) {
		body := `{"name":"` + strings.Repeat("a", (1<<20)+10) + `","url":"https://b.example"}`
		res, response := ts.do("POST", "/v0/nodes", body, headers)
		if res.StatusCode != http.StatusBadRequest || !strings.Contains(response, "too large") {
			t.Errorf("status %d body %s", res.StatusCode, response)
		}
	})
}

func TestStrictQueryParameters(t *testing.T) {
	ts := newTestServer(t)
	token := ts.bearer(t)
	headers := map[string]string{"Authorization": "Bearer " + token}

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"unknown", "GET", "/v0/nodes?nope=1"},
		{"repeated", "GET", "/v0/nodes?cluster=a&cluster=b"},
		{"semicolon", "GET", "/v0/nodes?a=1;b=2"},
		{"plus sign", "GET", "/v0/clusters/eu-west/health?days=%2B5"},
		{"leading zero", "GET", "/v0/clusters/eu-west/health?days=007"},
		{"bool word", "DELETE", "/v0/nodes/eu-west?force=1"},
		{"bool short", "DELETE", "/v0/nodes/eu-west?force=t"},
		{"empty bool", "DELETE", "/v0/nodes/eu-west?force="},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, body := ts.do(tc.method, tc.path, "", headers)
			if res.StatusCode != http.StatusBadRequest || !strings.Contains(body, `"code":"bad_request"`) {
				t.Errorf("status %d body %s", res.StatusCode, body)
			}
		})
	}

	// The accepted forms still reach the handler.
	res, body := ts.do("DELETE", "/v0/nodes/eu-west?force=false", "", headers)
	if res.StatusCode == http.StatusBadRequest {
		t.Errorf("force=false was refused: %s", body)
	}
}

func TestInvalidValueIs422WithField(t *testing.T) {
	ts := newTestServer(t)
	token := ts.bearer(t)
	headers := map[string]string{"Authorization": "Bearer " + token}

	res, body := ts.do("POST", "/v0/nodes", `{"name":"EU_West","url":"https://b.example"}`, headers)
	if res.StatusCode != http.StatusUnprocessableEntity || !strings.Contains(body, `"field":"name"`) {
		t.Fatalf("status %d body %s", res.StatusCode, body)
	}

	// The same name twice is a state conflict.
	res, body = ts.do("POST", "/v0/nodes", `{"name":"b","url":"https://b.example"}`, headers)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("first node: status %d body %s", res.StatusCode, body)
	}
	res, body = ts.do("POST", "/v0/nodes", `{"name":"b","url":"https://b2.example"}`, headers)
	if res.StatusCode != http.StatusConflict {
		t.Errorf("duplicate node: status %d body %s", res.StatusCode, body)
	}
}
