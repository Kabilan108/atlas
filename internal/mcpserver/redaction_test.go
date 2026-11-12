package mcpserver_test

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/mcpserver"
)

// Synthetic credentials with characters that change under base64, URL, and
// JSON escaping.
const (
	reflectEmail = "Synthetic.User@example.invalid"
	reflectToken = "AbC/def+ghi=0123456789"
)

// reflectingAPI answers every request with status and a body that echoes
// the caller's credentials in the encodings servers and proxies use.
func reflectingAPI(t *testing.T, status int, body func(r *http.Request) string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body(r)))
	}))
	t.Cleanup(server.Close)
	return server
}

func jsonEscapeAll(s string) string {
	var b strings.Builder
	for _, r := range s {
		fmt.Fprintf(&b, `\u%04x`, r)
	}
	return b.String()
}

func fold(s string, width int) string {
	var lines []string
	for len(s) > width {
		lines = append(lines, s[:width])
		s = s[width:]
	}
	return strings.Join(append(lines, s), "\r\n ")
}

// spells reports whether text still holds secret once whitespace is
// removed and JSON and percent escapes are decoded, as a reader could.
func spells(text, secret string) bool {
	compact := strings.Join(strings.Fields(text), "")
	unescaped := jsonEscape.ReplaceAllStringFunc(compact, func(m string) string {
		r, _ := strconv.ParseUint(m[2:], 16, 32)
		return string(rune(r))
	})
	unescaped = strings.ReplaceAll(unescaped, `\/`, "/")
	forms := []string{compact, unescaped}
	if decoded, err := url.QueryUnescape(unescaped); err == nil {
		forms = append(forms, decoded)
	}
	for _, form := range forms {
		if strings.Contains(form, secret) || strings.Contains(strings.ToLower(form), secret) {
			return true
		}
	}
	return false
}

var jsonEscape = regexp.MustCompile(`\\u[0-9a-fA-F]{4}`)

func TestToolErrorsNeverEchoCredentials(t *testing.T) {
	basic := base64.StdEncoding.EncodeToString([]byte(reflectEmail + ":" + reflectToken))
	urlBasic := base64.URLEncoding.EncodeToString([]byte(reflectEmail + ":" + reflectToken))
	secrets := []string{
		reflectToken, url.QueryEscape(reflectToken),
		basic, strings.TrimRight(basic, "="), urlBasic, strings.TrimRight(urlBasic, "="),
		strings.ToLower(reflectEmail), strings.ToLower(url.QueryEscape(reflectEmail)),
	}
	bodies := map[string]func(r *http.Request) string{
		"bitbucket error": func(r *http.Request) string {
			return fmt.Sprintf(`{"type":"error","error":{"message":"token %s rejected for %s","detail":"Authorization: %s","fields":{"%s":["%s"]}}}`,
				reflectToken, reflectEmail, r.Header.Get("Authorization"), reflectEmail, basic)
		},
		"json escapes": func(*http.Request) string {
			return fmt.Sprintf(`{"type":"error","error":{"message":"%s","detail":"%s"}}`, jsonEscapeAll(reflectToken), jsonEscapeAll(reflectEmail))
		},
		"unrecognized json": func(r *http.Request) string {
			return fmt.Sprintf(`{"echo":{"auth":"%s","user":"%s"}}`, jsonEscapeAll(r.Header.Get("Authorization")), jsonEscapeAll(reflectEmail))
		},
		"echoed request": func(r *http.Request) string {
			return fmt.Sprintf("GET %s HTTP/1.1\r\nAuthorization: Basic %s\r\nX-User: %s\r\nX-Query: token=%s&email=%s\r\n",
				r.URL.Path, fold(basic, 16), strings.ToUpper(reflectEmail), url.QueryEscape(reflectToken), url.QueryEscape(reflectEmail))
		},
		"wrapped basic": func(*http.Request) string {
			return "credential dump: " + fold(basic, 20)
		},
	}
	tools := []struct {
		name string
		args map[string]any
	}{
		{"whoami", nil},
		{"branch_view", map[string]any{"repo": "acme/widgets", "branch": "main"}},
		{"api", map[string]any{"path": "/2.0/user"}},
		{"jira_whoami", nil},
	}
	header := http.Header{
		mcpserver.HeaderEmail:          {reflectEmail},
		mcpserver.HeaderBitbucketToken: {reflectToken},
		mcpserver.HeaderJiraToken:      {reflectToken},
	}
	for name, body := range bodies {
		for _, status := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusBadGateway} {
			api := reflectingAPI(t, status, body)
			handler, err := mcpserver.Handler(mcpserver.Options{
				BitbucketOptions: []bitbucket.ClientOption{bitbucket.WithBaseURL(api.URL)},
				JiraOptions:      []jira.Option{jira.WithBaseURL(api.URL)},
			})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(handler)
			session := connect(t, server.URL+"/mcp", header)
			for _, tool := range tools {
				text, isError := callText(t, session, tool.name, tool.args)
				if !isError {
					t.Fatalf("%s %d %s: call succeeded: %q", name, status, tool.name, text)
				}
				for _, secret := range secrets {
					if spells(text, secret) {
						t.Errorf("%s %d %s: result spells a credential: %q", name, status, tool.name, text)
						break
					}
				}
			}
			server.Close()
		}
	}
}
