package bitbucket

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

const (
	// maxErrorBody bounds how much of a failed log response is read for its
	// error message.
	maxErrorBody = 64 << 10
	// maxErrorMessage bounds the message an APIError keeps from a body.
	maxErrorMessage = 4 << 10
)

func success(status int) bool {
	return status >= 200 && status < 300
}

// redactError scrubs a transport or URL error.
func (c *Client) redactError(err error) error {
	return c.redactor.Error(err)
}

// errorMessage is what of a failed response's body is safe to show:
// Bitbucket's error message, detail, and field errors from a JSON error, or
// the start of a text body, without the client's credentials. Any other
// JSON is dropped rather than kept raw, because its escapes can spell a
// credential that byte-level redaction cannot see. Decoded strings are
// redacted again for the same reason.
func (c *Client) errorMessage(body []byte) string {
	text := c.redactor.String(string(body))
	if json.Valid([]byte(text)) {
		return truncateMessage(c.redactor.String(bitbucketErrorMessage([]byte(text))))
	}
	return truncateMessage(strings.TrimSpace(text))
}

// jsonErrorMessage is errorMessage for a Bitbucket JSON error only.
func (c *Client) jsonErrorMessage(body []byte) string {
	text := c.redactor.String(string(body))
	if !json.Valid([]byte(text)) {
		return ""
	}
	return truncateMessage(c.redactor.String(bitbucketErrorMessage([]byte(text))))
}

func truncateMessage(message string) string {
	if len(message) > maxErrorMessage {
		return message[:maxErrorMessage] + "..."
	}
	return message
}

// checkResponse maps a failed response to an *APIError carrying only the
// safe part of its body.
func (c *Client) checkResponse(resp *http.Response, body []byte) error {
	if success(resp.StatusCode) {
		return nil
	}

	switch resp.StatusCode {
	case 401:
		return NewAuthError(401, "invalid credentials")
	case 403:
		return NewAuthError(403, "access denied")
	case 404:
		return NewNotFoundError("resource", extractResource(resp.Request.URL.Path))
	case 429:
		return NewRateLimitError(parseRateLimitReset(resp.Header))
	}
	message := c.errorMessage(body)
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	if resp.StatusCode >= 500 {
		return NewServerError(resp.StatusCode, message)
	}
	return &APIError{StatusCode: resp.StatusCode, Message: message, Resource: "api"}
}

// checkResponseWithMessage is checkResponse, but a 403 also carries
// Bitbucket's own message, which names the scope or permission missing.
func (c *Client) checkResponseWithMessage(resp *http.Response, body []byte) error {
	err := c.checkResponse(resp, body)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusForbidden {
		if message := c.jsonErrorMessage(body); message != "" {
			apiErr.Message = message
		}
	}
	return err
}

func bitbucketErrorMessage(body []byte) string {
	var payload struct {
		Error struct {
			Message string              `json:"message"`
			Detail  json.RawMessage     `json:"detail"`
			Fields  map[string][]string `json:"fields"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}

	message := strings.TrimSpace(payload.Error.Message)
	var detail string
	if len(payload.Error.Detail) > 0 && json.Unmarshal(payload.Error.Detail, &detail) == nil {
		if detail = strings.TrimSpace(detail); detail != "" && detail != message {
			if message == "" {
				message = detail
			} else {
				message += ": " + detail
			}
		}
	}
	names := make([]string, 0, len(payload.Error.Fields))
	for name := range payload.Error.Fields {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := []string{}
	if message != "" {
		parts = append(parts, message)
	}
	for _, name := range names {
		parts = append(parts, name+": "+strings.Join(payload.Error.Fields[name], ", "))
	}
	return strings.Join(parts, "; ")
}

// withoutQuery is a URL error's text without the query, which for a
// presigned storage URL is a short-lived credential of its own.
func withoutQuery(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	if parsed, parseErr := url.Parse(urlErr.URL); parseErr == nil && parsed.RawQuery != "" {
		parsed.RawQuery = ""
		urlErr.URL = parsed.Redacted()
	}
	return err
}
