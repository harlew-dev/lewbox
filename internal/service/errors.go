package service

import (
	"fmt"
	"time"
)

// Code classifies an Error. httpapi maps each code to an HTTP status, and the
// CLI prints the same {error: {code, message, details}} body.
type Code string

const (
	CodeBadRequest   Code = "bad_request"         // 400: the request cannot be read
	CodeInvalid      Code = "invalid_request"     // 422: a value breaks a rule, or a quota is reached
	CodeUnauthorized Code = "unauthorized"        // 401
	CodeForbidden    Code = "forbidden"           // 403
	CodeNotFound     Code = "not_found"           // 404
	CodeConflict     Code = "conflict"            // 409
	CodePrecondition Code = "precondition_failed" // CLI machine checks only; never an HTTP status
	CodeRateLimited  Code = "rate_limited"        // 429
	CodeUnavailable  Code = "unavailable"         // 503: a dependency is down
)

// Error is an expected failure that is safe to show the caller. Any other
// error returned by Service is internal.
type Error struct {
	Code    Code
	Message string
	Details any
}

func (e *Error) Error() string { return e.Message }

// badRequest reports a request that cannot be read: malformed JSON, an unknown
// field, or a malformed query parameter.
func badRequest(format string, a ...any) error {
	return &Error{Code: CodeBadRequest, Message: fmt.Sprintf(format, a...)}
}

// invalid reports a parsed value that breaks a rule, or a quota that is
// reached. Name the one field at fault with invalidField.
func invalid(format string, a ...any) error {
	return &Error{Code: CodeInvalid, Message: fmt.Sprintf(format, a...)}
}

// FieldDetails names the one request field an invalid error is about.
type FieldDetails struct {
	Field string `json:"field"`
}

// invalidField is invalid for a value that breaks a rule on one field, named
// as in the request body.
func invalidField(field, format string, a ...any) error {
	return &Error{Code: CodeInvalid, Message: fmt.Sprintf(format, a...), Details: FieldDetails{Field: field}}
}

func unauthorized(format string, a ...any) error {
	return &Error{Code: CodeUnauthorized, Message: fmt.Sprintf(format, a...)}
}

func forbidden(format string, a ...any) error {
	return &Error{Code: CodeForbidden, Message: fmt.Sprintf(format, a...)}
}

func notFound(format string, a ...any) error {
	return &Error{Code: CodeNotFound, Message: fmt.Sprintf(format, a...)}
}

func conflict(format string, a ...any) error {
	return &Error{Code: CodeConflict, Message: fmt.Sprintf(format, a...)}
}

// precondition reports a local machine check that failed before the work could
// start. It is for CLI-only commands; API routes use conflict, invalid or
// unavailable instead.
func precondition(details any, format string, a ...any) error {
	return &Error{Code: CodePrecondition, Message: fmt.Sprintf(format, a...), Details: details}
}

func unavailable(format string, a ...any) error {
	return &Error{Code: CodeUnavailable, Message: fmt.Sprintf(format, a...)}
}

// rateLimited reports an exhausted budget and, in details, how many whole
// seconds the caller should wait before trying again.
func rateLimited(retryAfter time.Duration, format string, a ...any) error {
	seconds := int((retryAfter + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return &Error{
		Code:    CodeRateLimited,
		Message: fmt.Sprintf(format, a...),
		Details: map[string]any{"retry_after_seconds": seconds},
	}
}
