package jira

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrUnauthorized = errors.New("unauthorized")
	ErrForbidden    = errors.New("forbidden")
	ErrNotFound     = errors.New("not found")
	ErrRateLimited  = errors.New("rate limited")
	ErrServerError  = errors.New("server error")
)

// APIError is a non-2xx response from Jira. ErrorMessages and Errors hold
// Jira's error collection; Service Management's errorMessage is folded into
// ErrorMessages. Body keeps the raw response (truncated) when it held neither.
type APIError struct {
	StatusCode    int
	Method        string
	Path          string
	ErrorMessages []string
	Errors        map[string]string
	Body          string
	RetryAfter    time.Duration
}

func (e *APIError) HTTPStatus() int {
	return e.StatusCode
}

func (e *APIError) Error() string {
	var parts []string
	parts = append(parts, e.ErrorMessages...)
	keys := make([]string, 0, len(e.Errors))
	for k := range e.Errors {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		parts = append(parts, k+": "+e.Errors[k])
	}
	if len(parts) == 0 && e.Body != "" {
		parts = append(parts, e.Body)
	}
	if len(parts) == 0 {
		parts = append(parts, http.StatusText(e.StatusCode))
	}
	return fmt.Sprintf("jira: %s %s: %d: %s", e.Method, e.Path, e.StatusCode, strings.Join(parts, "; "))
}

func (e *APIError) Unwrap() error {
	switch {
	case e.StatusCode == http.StatusUnauthorized:
		return ErrUnauthorized
	case e.StatusCode == http.StatusForbidden:
		return ErrForbidden
	case e.StatusCode == http.StatusNotFound:
		return ErrNotFound
	case e.StatusCode == http.StatusTooManyRequests:
		return ErrRateLimited
	case e.StatusCode >= 500:
		return ErrServerError
	}
	return nil
}

func newAPIError(req *http.Request, resp *http.Response, body []byte) *APIError {
	e := &APIError{
		StatusCode: resp.StatusCode,
		Method:     req.Method,
		Path:       req.URL.Path,
		RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
	}
	var wire struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
		ErrorMessage  string            `json:"errorMessage"`
		Message       string            `json:"message"`
	}
	if json.Unmarshal(body, &wire) == nil {
		e.ErrorMessages = wire.ErrorMessages
		if wire.ErrorMessage != "" {
			e.ErrorMessages = append(e.ErrorMessages, wire.ErrorMessage)
		}
		if wire.Message != "" && len(e.ErrorMessages) == 0 {
			e.ErrorMessages = append(e.ErrorMessages, wire.Message)
		}
		if len(wire.Errors) > 0 {
			e.Errors = wire.Errors
		}
	}
	if len(e.ErrorMessages) == 0 && len(e.Errors) == 0 {
		text := strings.TrimSpace(string(body))
		if len(text) > maxErrorBody {
			text = text[:maxErrorBody] + "..."
		}
		e.Body = text
	}
	return e
}

func parseRetryAfter(value string) time.Duration {
	if value == "" {
		return 0
	}
	if secs, err := strconv.Atoi(value); err == nil {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(value); err == nil {
		return time.Until(t)
	}
	return 0
}
