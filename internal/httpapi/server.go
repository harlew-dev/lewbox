package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/harlew-dev/lewbox/internal/logsafe"
	"github.com/harlew-dev/lewbox/internal/service"
)

// maxBodyBytes caps JSON request bodies.
const maxBodyBytes = 1 << 20

// defaultRetryAfter is how many seconds a 503 asks the caller to wait when the
// error does not name its own delay.
const defaultRetryAfter = 30

// server holds what every handler needs: the service, the logger and the
// router's options (cookies).
type server struct {
	svc  *service.Service
	log  *slog.Logger
	opts RouterOptions
}

// decode reads the request's JSON body into v.
func (s *server) decode(w http.ResponseWriter, r *http.Request, v any) error {
	return decodeJSON(w, r, v)
}

// respond writes a result or the mapped error.
func (s *server) respond(w http.ResponseWriter, status int, v any, err error) {
	respond(w, s.log, status, v, err)
}

func respond(w http.ResponseWriter, log *slog.Logger, status int, v any, err error) {
	if err != nil {
		status, body := ErrorFrom(err)
		if status == http.StatusInternalServerError {
			// The real error goes to the log under the request ID; the body
			// carries only the generic sentence and that ID.
			id := w.Header().Get(requestIDHeader)
			body.Error.Message = internalMessage
			body.Error.Details = nil
			if id != "" {
				body.Error.Details = map[string]string{"request_id": id}
			}
			if log != nil {
				log.Error("request failed", "request_id", id, "err", logsafe.ScrubError(err))
			}
		}
		if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
			seconds := defaultRetryAfter
			if got, ok := retryAfterSeconds(body.Error.Details); ok {
				seconds = got
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
		}
		writeJSON(w, status, body)
		return
	}
	writeJSON(w, status, v)
}

// internalMessage is the only text a 500 ever carries.
const internalMessage = "Something went wrong. Please try again."

// retryAfterSeconds reads the retry_after_seconds detail of a rate_limited
// error.
func retryAfterSeconds(details any) (int, bool) {
	m, ok := details.(map[string]any)
	if !ok {
		return 0, false
	}
	switch v := m["retry_after_seconds"].(type) {
	case int:
		return v, v > 0
	case int64:
		return int(v), v > 0
	case float64:
		return int(v), v > 0
	default:
		return 0, false
	}
}

// decodeJSON requires exactly one JSON object. Shape problems are 400s with a
// fixed sentence; Go's parser text never reaches the caller.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		return bodyError(err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return badBody("the request body must contain exactly one JSON object")
	}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return badBody("the request body must be one JSON object")
	}
	inner := json.NewDecoder(bytes.NewReader(trimmed))
	inner.DisallowUnknownFields()
	if err := inner.Decode(v); err != nil {
		return bodyError(err)
	}
	return nil
}

func bodyError(err error) error {
	var maxErr *http.MaxBytesError
	switch {
	case errors.As(err, &maxErr):
		return badBody("the request body is too large")
	case errors.Is(err, io.EOF):
		return badBody("the request body is required")
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return badBody("the request body is not valid JSON")
	}
	if strings.Contains(err.Error(), "unknown field") {
		return badBody("the request body contains an unknown field")
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		return badBody("a field in the request body has the wrong type")
	}
	return badBody("the request body is not valid JSON")
}

func badBody(message string) error {
	return &service.Error{Code: service.CodeBadRequest, Message: message}
}

// requestOrganization resolves the organization a fleet-capable route acts
// on: the org query parameter when present, otherwise the header.
func requestOrganization(r *http.Request) string {
	if org := strings.TrimSpace(r.URL.Query().Get("org")); org != "" {
		return org
	}
	return r.Header.Get(OrganizationHeader)
}

// queryBool parses an optional boolean query parameter. Only the words true
// and false are accepted; an empty value is not false.
func queryBool(r *http.Request, name string) (bool, error) {
	values := r.URL.Query()
	if !values.Has(name) {
		return false, nil
	}
	switch values.Get(name) {
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, badBody(name + " must be true or false")
	}
}

// queryInt parses an optional whole-number query parameter. Digits only: a
// sign, a space, a decimal point or a leading zero are all refused.
func queryInt(r *http.Request, name string, fallback int) (int, error) {
	values := r.URL.Query()
	if !values.Has(name) {
		return fallback, nil
	}
	raw := values.Get(name)
	if !strictDigits(raw) {
		return 0, badBody(name + " must be a whole number")
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, badBody(name + " must be a whole number")
	}
	return n, nil
}

// strictDigits reports whether raw is plain decimal digits without a leading
// zero (except "0" itself).
func strictDigits(raw string) bool {
	if raw == "" {
		return false
	}
	for i, c := range raw {
		if c < '0' || c > '9' {
			return false
		}
		if i == 0 && c == '0' && len(raw) > 1 {
			return false
		}
	}
	return true
}

// ErrorBody is the API's error shape: {error: {code, message, details}}.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail is the inner object of ErrorBody.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// ErrorFrom maps an error from the service to an HTTP status and error
// body. Errors that aren't *service.Error are internal and their text is
// not exposed.
func ErrorFrom(err error) (int, ErrorBody) {
	se, ok := errors.AsType[*service.Error](err)
	if !ok {
		return http.StatusInternalServerError, ErrorBody{Error: ErrorDetail{Code: "internal", Message: internalMessage}}
	}
	status := http.StatusInternalServerError
	switch se.Code {
	case service.CodeBadRequest:
		status = http.StatusBadRequest
	case service.CodeInvalid, service.CodePrecondition:
		status = http.StatusUnprocessableEntity
	case service.CodeUnauthorized:
		status = http.StatusUnauthorized
	case service.CodeForbidden:
		status = http.StatusForbidden
	case service.CodeNotFound:
		status = http.StatusNotFound
	case service.CodeConflict:
		status = http.StatusConflict
	case service.CodeRateLimited:
		status = http.StatusTooManyRequests
	case service.CodeUnavailable:
		status = http.StatusServiceUnavailable
	}
	body := ErrorBody{Error: ErrorDetail{Code: string(se.Code), Message: se.Message, Details: se.Details}}
	if status == http.StatusInternalServerError {
		body.Error.Message = internalMessage
		body.Error.Details = nil
	}
	return status, body
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = Encode(w, v)
}

// Encode writes v as one line of JSON. The CLI uses it too, so both entry
// points produce byte-identical output.
func Encode(w io.Writer, v any) error {
	return json.NewEncoder(w).Encode(v)
}
