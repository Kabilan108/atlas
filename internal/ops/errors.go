package ops

import (
	"errors"
	"fmt"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

// Kind classifies an error so each front end can choose its own exit code,
// hint, or tool error text.
type Kind uint8

const (
	Unknown Kind = iota
	Invalid
	Auth
	NotFound
	RateLimited
	Upstream
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
	case RateLimited:
		return "rate_limited"
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

// KindOf returns the kind of the outermost classified error in err's chain.
// Bitbucket API errors are classified by HTTP status.
func KindOf(err error) Kind {
	if err == nil {
		return Unknown
	}
	var opErr *Error
	if errors.As(err, &opErr) {
		return opErr.Kind
	}
	var apiErr *bitbucket.APIError
	if errors.As(err, &apiErr) {
		return kindForStatus(apiErr.StatusCode)
	}
	return Unknown
}

func kindForStatus(status int) Kind {
	switch {
	case status == 401 || status == 403:
		return Auth
	case status == 404:
		return NotFound
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
