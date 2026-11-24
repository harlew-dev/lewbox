package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/harlew-dev/lewbox/internal/service"
)

func TestErrorFromStatuses(t *testing.T) {
	cases := []struct {
		code   service.Code
		status int
	}{
		{service.CodeBadRequest, http.StatusBadRequest},
		{service.CodeUnauthorized, http.StatusUnauthorized},
		{service.CodeForbidden, http.StatusForbidden},
		{service.CodeNotFound, http.StatusNotFound},
		{service.CodeConflict, http.StatusConflict},
		{service.CodeInvalid, http.StatusUnprocessableEntity},
		{service.CodeRateLimited, http.StatusTooManyRequests},
		{service.CodeUnavailable, http.StatusServiceUnavailable},
	}
	for _, tc := range cases {
		status, body := ErrorFrom(&service.Error{Code: tc.code, Message: "message"})
		if status != tc.status || body.Error.Code != string(tc.code) {
			t.Errorf("%s = %d %q, want %d", tc.code, status, body.Error.Code, tc.status)
		}
	}
}

func TestInternalErrorHidesTextAndCarriesRequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set(requestIDHeader, "req_abcdefghijklmn")
	respond(rec, slog.New(slog.DiscardHandler), 0, nil, errors.New("dial tcp: secret detail"))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	var body ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Message != internalMessage || body.Error.Code != "internal" {
		t.Errorf("body = %+v", body)
	}
	details, ok := body.Error.Details.(map[string]any)
	if !ok || details["request_id"] != "req_abcdefghijklmn" {
		t.Errorf("details = %#v", body.Error.Details)
	}
	if strings.Contains(rec.Body.String(), "secret detail") {
		t.Errorf("body leaks the error: %s", rec.Body.String())
	}
}

func TestUnavailableSetsRetryAfter(t *testing.T) {
	rec := httptest.NewRecorder()
	respond(rec, slog.New(slog.DiscardHandler), 0, nil, &service.Error{Code: service.CodeUnavailable, Message: "down"})
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "30" {
		t.Errorf("status %d Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestRateLimitKeepsItsOwnRetryAfter(t *testing.T) {
	rec := httptest.NewRecorder()
	respond(rec, slog.New(slog.DiscardHandler), 0, nil, &service.Error{
		Code: service.CodeRateLimited, Message: "slow down",
		Details: map[string]any{"retry_after_seconds": 7},
	})
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "7" {
		t.Errorf("status %d Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestRecoveryAnswersGeneric500(t *testing.T) {
	panicking := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("secret panic text lbs_abc123")
	})
	handler := withRequestID(withRecovery(panicking, slog.New(slog.DiscardHandler)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/v0/health", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", rec.Code)
	}
	id := rec.Header().Get(requestIDHeader)
	if !strings.HasPrefix(id, "req_") || len(id) != len("req_")+14 {
		t.Fatalf("request ID = %q", id)
	}
	if !strings.Contains(rec.Body.String(), `"request_id":"`+id+`"`) || strings.Contains(rec.Body.String(), "secret") {
		t.Errorf("body = %s", rec.Body.String())
	}
}
