package bitbucket

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrNotFound     = errors.New("not found")
	ErrRateLimited  = errors.New("rate limited")
	ErrServerError  = errors.New("server error")
)

const (
	ExitSuccess       = 0
	ExitGeneralError  = 1
	ExitAuthError     = 4
	ExitNotFoundError = 5
	ExitRateLimited   = 6
)

type APIError struct {
	StatusCode int
	Message    string
	Resource   string
	Hint       string
}

func (e *APIError) Error() string {
	if e.Hint != "" {
		return fmt.Sprintf("%s: %s\nHint: %s", e.Resource, e.Message, e.Hint)
	}
	return fmt.Sprintf("%s: %s", e.Resource, e.Message)
}

func (e *APIError) Unwrap() error {
	switch e.StatusCode {
	case 401:
		return ErrUnauthorized
	case 403:
		return ErrForbidden
	case 404:
		return ErrNotFound
	case 429:
		return ErrRateLimited
	default:
		if e.StatusCode >= 500 {
			return ErrServerError
		}
		return nil
	}
}

func (e *APIError) ExitCode() int {
	switch e.StatusCode {
	case 401, 403:
		return ExitAuthError
	case 404:
		return ExitNotFoundError
	case 429:
		return ExitRateLimited
	default:
		return ExitGeneralError
	}
}

func NewAuthError(statusCode int, message string) *APIError {
	return &APIError{
		StatusCode: statusCode,
		Message:    message,
		Resource:   "authentication",
		Hint:       "Run 'atlas config verify' to check your credentials",
	}
}

func NewNotFoundError(resource, identifier string) *APIError {
	return &APIError{
		StatusCode: 404,
		Message:    fmt.Sprintf("%s '%s' not found", resource, identifier),
		Resource:   resource,
		Hint:       fmt.Sprintf("Check that the %s exists and you have access to it", resource),
	}
}

func NewRateLimitError(resetTime time.Time) *APIError {
	waitDuration := time.Until(resetTime).Round(time.Second)
	hint := fmt.Sprintf("Rate limit resets in %s. Use --retry to automatically wait and retry", waitDuration)
	if waitDuration <= 0 {
		hint = "Rate limit should reset soon. Use --retry to automatically retry"
	}

	return &APIError{
		StatusCode: 429,
		Message:    "API rate limit exceeded",
		Resource:   "rate limit",
		Hint:       hint,
	}
}

func NewServerError(statusCode int, message string) *APIError {
	return &APIError{
		StatusCode: statusCode,
		Message:    message,
		Resource:   "server",
		Hint:       "This is a Bitbucket server error. Try again later",
	}
}

func ExitCodeFromError(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.ExitCode()
	}
	if err != nil {
		return ExitGeneralError
	}
	return ExitSuccess
}
