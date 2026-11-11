package service

import (
	"fmt"
	"time"
)

// Code classifies an Error. httpapi maps each code to an HTTP status, and the
// CLI prints the same {error: {code, message, details}} body.
type Code string

const (
	CodeInvalid      Code = "invalid_request"     // 400
	CodeUnauthorized Code = "unauthorized"        // 401
	CodeForbidden    Code = "forbidden"           // 403
	CodeNotFound     Code = "not_found"           // 404
	CodeConflict     Code = "conflict"            // 409
	CodePrecondition Code = "precondition_failed" // 412
	CodeRateLimited  Code = "rate_limited"        // 429
	CodeUnavailable  Code = "unavailable"         // 502
)

// Error is an expected failure that is safe to show the caller. Any other
// error returned by Service is internal.
type Error struct {
	Code    Code
	Message string
	Details any
}

func (e *Error) Error() string { return e.Message }

func invalid(format string, a ...any) error {
	return &Error{Code: CodeInvalid, Message: fmt.Sprintf(format, a...)}
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

// precondition reports a check that failed before the work could start.
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
