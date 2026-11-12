package bitbucket

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const (
	redactEmail = "Synthetic.User@example.invalid"
	redactToken = "AbC/def+ghi=0123456789"
)

func escapeEvery(s string) string {
	var b strings.Builder
	for _, r := range s {
		fmt.Fprintf(&b, `\u%04x`, r)
	}
	return b.String()
}

func wrapLines(s string, width int) string {
	var lines []string
	for len(s) > width {
		lines = append(lines, s[:width])
		s = s[width:]
	}
	return strings.Join(append(lines, s), "\r\n ")
}

// clientCalls reaches every way the client reads a response: the cached
// GET, raw GET, JSON writes, the uncached send, the limited reader, file
// content, and multipart writes.
var clientCalls = map[string]func(*Client) error{
	"get": func(c *Client) error {
		_, err := c.GetCurrentUser(context.Background())
		return err
	},
	"getRaw": func(c *Client) error {
		_, err := c.GetPullRequestDiff(context.Background(), "ws", "repo", 7)
		return err
	},
	"sendJSON": func(c *Client) error {
		_, err := c.CreatePullRequest(context.Background(), "ws", "repo", PullRequestCreate{Title: "t"})
		return err
	},
	"send": func(c *Client) error {
		_, err := c.GetBranch(context.Background(), "ws", "repo", "main")
		return err
	},
	"getLimited": func(c *Client) error {
		_, err := c.GetAPI(context.Background(), "/user", 1<<20)
		return err
	},
	"fileContent": func(c *Client) error {
		_, err := c.GetFileContent(context.Background(), "ws", "repo", "main", "README.md")
		return err
	},
	"multipart": func(c *Client) error {
		_, err := c.CreateSnippet(context.Background(), "ws", "t", map[string][]byte{"a.txt": []byte("a")}, true)
		return err
	},
}

func TestAPIErrorsRedactCredentials(t *testing.T) {
	basic := base64.StdEncoding.EncodeToString([]byte(redactEmail + ":" + redactToken))
	urlBasic := base64.URLEncoding.EncodeToString([]byte(redactEmail + ":" + redactToken))
	secrets := []string{
		redactToken, url.QueryEscape(redactToken), url.PathEscape(redactToken),
		basic, strings.TrimRight(basic, "="), urlBasic, strings.TrimRight(urlBasic, "="),
		strings.ToLower(redactEmail), strings.ToLower(url.QueryEscape(redactEmail)),
		"dynamic-bearer-credential",
	}
	bodies := map[string]string{
		"plain token":         "bad token " + redactToken,
		"basic padded":        "Basic " + basic,
		"basic unpadded":      strings.TrimRight(basic, "="),
		"basic url-safe":      strings.TrimRight(urlBasic, "="),
		"query-escaped":       "token=" + url.QueryEscape(redactToken) + "&user=" + url.QueryEscape(redactEmail),
		"email any case":      "no access for " + strings.ToUpper(redactEmail),
		"authorization label": "Authorization: Bearer dynamic-bearer-credential",
		"folded label":        "Authorization: Bearer\r\n dynamic-bearer-credential",
		"wrapped basic":       "Authorization: Basic " + wrapLines(basic, 16),
		"wrapped unlabelled":  "dump " + wrapLines(basic, 20),
		"json message":        `{"type":"error","error":{"message":"` + escapeEvery(redactToken) + `"}}`,
		"json detail":         `{"type":"error","error":{"message":"bad","detail":"` + escapeEvery(redactEmail) + `"}}`,
		"json fields":         `{"type":"error","error":{"message":"bad","fields":{"` + escapeEvery(redactToken) + `":["` + basic + `"]}}}`,
		"json unrecognized":   `{"echo":"` + escapeEvery("Basic "+basic) + `"}`,
		"truncation boundary": strings.Repeat("x", 4091) + redactToken,
	}
	for name, body := range bodies {
		for call, run := range clientCalls {
			for _, status := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusConflict, http.StatusBadGateway} {
				transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
					r.Header.Set("Authorization", "Bearer dynamic-bearer-credential")
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
						Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
				})
				client, err := NewClientWithCredentials(redactEmail, redactToken,
					WithBaseURL("https://api.bitbucket.org/2.0"), WithHTTPClient(&http.Client{Transport: transport}))
				if err != nil {
					t.Fatal(err)
				}
				err = run(client)
				var apiErr *APIError
				if !errors.As(err, &apiErr) || apiErr.StatusCode != status {
					t.Fatalf("%s %s %d: err = %v, want an APIError with the status", name, call, status, err)
				}
				dump := err.Error() + fmt.Sprintf("%#v", *apiErr)
				for _, secret := range secrets {
					if spellsSecret(dump, secret) {
						t.Errorf("%s %s %d: error spells a credential: %q", name, call, status, dump)
						break
					}
				}
			}
		}
	}
}

func TestTransportErrorsRedactCredentials(t *testing.T) {
	basic := base64.StdEncoding.EncodeToString([]byte(redactEmail + ":" + redactToken))
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("proxy refused %s with Authorization: %s for %s", r.URL.Path, r.Header.Get("Authorization"), redactEmail)
	})
	client, err := NewClientWithCredentials(redactEmail, redactToken,
		WithBaseURL("https://api.bitbucket.org/2.0"), WithHTTPClient(&http.Client{Transport: transport}))
	if err != nil {
		t.Fatal(err)
	}
	for call, run := range clientCalls {
		err := run(client)
		if err == nil {
			t.Fatalf("%s: no error", call)
		}
		var urlErr *url.Error
		if !errors.As(err, &urlErr) {
			t.Fatalf("%s: the transport error left the chain: %v", call, err)
		}
		for _, secret := range []string{redactToken, basic, strings.ToLower(redactEmail)} {
			if spellsSecret(err.Error(), secret) {
				t.Errorf("%s: error spells a credential: %q", call, err)
			}
		}
	}
}

// spellsSecret reports whether text holds secret once whitespace is removed
// and JSON and percent escapes are decoded, as a reader could.
func spellsSecret(text, secret string) bool {
	compact := strings.Join(strings.Fields(text), "")
	var unescaped strings.Builder
	for i := 0; i < len(compact); i++ {
		if strings.HasPrefix(compact[i:], `\u`) && i+6 <= len(compact) {
			var r rune
			if _, err := fmt.Sscanf(compact[i+2:i+6], "%04x", &r); err == nil {
				unescaped.WriteRune(r)
				i += 5
				continue
			}
		}
		unescaped.WriteByte(compact[i])
	}
	forms := []string{compact, unescaped.String()}
	if decoded, err := url.QueryUnescape(unescaped.String()); err == nil {
		forms = append(forms, decoded)
	}
	for _, form := range forms {
		if strings.Contains(form, secret) || strings.Contains(strings.ToLower(form), secret) {
			return true
		}
	}
	return false
}
