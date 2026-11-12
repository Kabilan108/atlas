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
// ErrorMessages. Body keeps a redacted, truncated copy of a non-JSON response
// when it held neither; unrecognized JSON bodies are not kept.
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

// redact removes the client's credentials, in any encoding it sends them,
// and the value of anything labelled Authorization; see redact.Credentials.
func (c *Client) redact(s string) string {
	return c.redactor.String(s)
}

// newAPIError builds the error from a redacted copy of the body and redacts
// every decoded string again, since JSON escapes can spell a secret that the
// raw bytes do not contain.
func newAPIError(req *http.Request, resp *http.Response, body []byte, redact func(string) string) *APIError {
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
	text := redact(string(body))
	isJSON := json.Valid([]byte(text))
	if isJSON && json.Unmarshal([]byte(text), &wire) == nil {
		for _, m := range wire.ErrorMessages {
			e.ErrorMessages = append(e.ErrorMessages, redact(m))
		}
		if wire.ErrorMessage != "" {
			e.ErrorMessages = append(e.ErrorMessages, redact(wire.ErrorMessage))
		}
		if wire.Message != "" && len(e.ErrorMessages) == 0 {
			e.ErrorMessages = append(e.ErrorMessages, redact(wire.Message))
		}
		if len(wire.Errors) > 0 {
			e.Errors = make(map[string]string, len(wire.Errors))
			for k, v := range wire.Errors {
				e.Errors[redact(k)] = redact(v)
			}
		}
	}
	// JSON outside Jira's error fields is dropped rather than kept raw: its
	// escapes can spell a credential that byte-level redaction cannot see.
	if len(e.ErrorMessages) == 0 && len(e.Errors) == 0 && !isJSON {
		text = strings.TrimSpace(text)
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
