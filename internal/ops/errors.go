package ops

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// Kind classifies an error so each front end can choose its own exit code,
// hint, or tool error text.
type Kind uint8

const (
	Unknown Kind = iota
	Invalid
	Auth
	NotFound
	Conflict
	RateLimited
	Timeout
	Upstream
	// Partial means the op's output is usable but incomplete; see Op.
	Partial
)

func (k Kind) String() string {
	switch k {
	case Invalid:
		return "invalid"
	case Auth:
		return "auth"
	case NotFound:
		return "not_found"
	case Conflict:
		return "conflict"
	case RateLimited:
		return "rate_limited"
	case Timeout:
		return "timeout"
	case Upstream:
		return "upstream"
	case Partial:
		return "partial"
	default:
		return "unknown"
	}
}

// Error is an error with a Kind.
type Error struct {
	Kind Kind
	Err  error
}

func (e *Error) Error() string {
	return e.Err.Error()
}

func (e *Error) Unwrap() error {
	return e.Err
}

// Errorf builds an Error of kind. It accepts %w like fmt.Errorf.
func Errorf(kind Kind, format string, args ...any) error {
	return &Error{Kind: kind, Err: fmt.Errorf(format, args...)}
}

// PartialOf is the error a batch op returns with its per-entry results when
// failed of total entries failed, including when all of them did, so callers
// always get the results that say which entries to retry. It returns nil when
// nothing failed. Truncated output is not a failure and never uses it.
func PartialOf(failed, total int, noun string) error {
	if failed == 0 {
		return nil
	}
	return Errorf(Partial, "%d of %d %s failed; see the per-entry results for which to retry", failed, total, noun)
}

// HTTPStatusError is implemented by API errors from both clients
// (bitbucket.APIError, jira.APIError), so classifying them needs neither.
type HTTPStatusError interface {
	error
	HTTPStatus() int
}

// KindOf returns the kind of the outermost classified error in err's chain:
// an *Error's kind, Timeout for a deadline, or the kind an HTTP status maps
// to.
func KindOf(err error) Kind {
	if err == nil {
		return Unknown
	}
	var opErr *Error
	if errors.As(err, &opErr) {
		return opErr.Kind
	}
	if isTimeout(err) {
		return Timeout
	}
	var statusErr HTTPStatusError
	if errors.As(err, &statusErr) {
		return kindForStatus(statusErr.HTTPStatus())
	}
	return Unknown
}

func isTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}

func kindForStatus(status int) Kind {
	switch {
	case status == 401 || status == 403:
		return Auth
	case status == 404:
		return NotFound
	case status == 408:
		return Timeout
	case status == 409:
		return Conflict
	case status == 429:
		return RateLimited
	case status >= 500:
		return Upstream
	case status >= 400:
		return Invalid
	default:
		return Upstream
	}
}
