package jira

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestRedactionRedirectMatrix(t *testing.T) {
	for _, tc := range []struct {
		name, base string
		targets    []string
		allowed    bool
	}{
		{"chain-cross-host", "https://jira.example.com", []string{"/middle", "https://evil.jira.example.com/end"}, false},
		{"chain-cross-port", "https://jira.example.com", []string{"/middle", "https://jira.example.com:8443/end"}, false},
		{"chain-downgrade", "https://jira.example.com", []string{"/middle", "http://jira.example.com/end"}, false},
		{"uppercase-host", "https://jira.example.com", []string{"https://JIRA.EXAMPLE.COM/end"}, true},
		{"uppercase-base", "https://JIRA.EXAMPLE.COM", []string{"https://jira.example.com/end"}, true},
		{"explicit-443", "https://jira.example.com", []string{"https://jira.example.com:443/end"}, true},
		{"implicit-443", "https://jira.example.com:443", []string{"https://jira.example.com/end"}, true},
		{"userinfo-same-host", "https://jira.example.com", []string{"https://other:password@jira.example.com/end"}, true},
		{"userinfo-evil-host", "https://jira.example.com", []string{"https://jira.example.com:password@evil.example/end"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, policy := 0, 0
			hc := &http.Client{CheckRedirect: func(r *http.Request, via []*http.Request) error { policy++; return nil }}
			hc.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				// Model a transport that adds auth itself before sending any request.
				r.Header.Set("Authorization", "Bearer synthetic-transport-credential")
				calls++
				if calls <= len(tc.targets) {
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": {tc.targets[calls-1]}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"accountId":"ok"}`)), Request: r}, nil
			})
			c, err := NewClientWithCredentials(testEmail, testToken, "", WithBaseURL(tc.base), WithHTTPClient(hc))
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Myself(context.Background())
			wantCalls := len(tc.targets) + 1
			wantPolicy := len(tc.targets)
			if !tc.allowed {
				wantCalls--
				wantPolicy--
			}
			if calls != wantCalls || policy != wantPolicy || (err == nil) != tc.allowed {
				t.Fatalf("calls=%d want=%d policy=%d want=%d err=%v", calls, wantCalls, policy, wantPolicy, err)
			}
			if c.httpClient == hc {
				t.Fatal("client was not cloned")
			}
		})
	}
}

func TestRedactionCredentialMatrix(t *testing.T) {
	const token = "AbC/def+ghi=0123456789"
	const email = "synthetic@example.invalid"
	const transportToken = "dynamic-bearer-credential"
	basic := base64.StdEncoding.EncodeToString([]byte(email + ":" + token))
	escape := func(s string) string { return strings.ReplaceAll(s, "A", `\u0041`) }
	wrap := func(s string, width int) string {
		var out []string
		for len(s) > width {
			out = append(out, s[:width])
			s = s[width:]
		}
		return strings.Join(append(out, s), "\r\n ")
	}
	cases := []struct {
		name, body         string
		decodeJSON, unfold bool
	}{
		{"plain-token", token, false, false},
		{"basic-padded", basic, false, false},
		{"basic-unpadded", strings.TrimRight(basic, "="), false, false},
		{"query-escaped-token", "AbC%2Fdef%2Bghi%3D0123456789", false, false},
		{"json-escaped-message", `{"message":"` + escape(token) + `"}`, true, false},
		{"json-escaped-errors-key", `{"errors":{"` + escape(token) + `":"bad"}}`, true, false},
		{"json-escaped-fallback", `{"detail":"` + escape(token) + `"}`, true, false},
		{"json-escaped-fallback-key", `{"` + escape(token) + `":"bad"}`, true, false},
		{"truncation-boundary", strings.Repeat("x", maxErrorBody-5) + token, false, false},
		{"known-bearer", "Authorization: Bearer " + token, false, false},
		{"custom-bearer", "Authorization: Bearer " + transportToken, false, false},
		{"folded-custom-bearer", "Authorization: Bearer\r\n " + transportToken, false, true},
	}
	for _, width := range []int{16, 32, 40} {
		cases = append(cases, struct {
			name, body         string
			decodeJSON, unfold bool
		}{fmt.Sprintf("wrapped-basic-%d", width), "Authorization: Basic " + wrap(basic, width), false, true})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				r.Header.Set("Authorization", "Bearer "+transportToken)
				return &http.Response{StatusCode: 502, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})}
			c, err := NewClientWithCredentials(email, token, "", WithBaseURL("https://jira.example.com"), WithHTTPClient(hc))
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Myself(context.Background())
			var ae *APIError
			if !errors.As(err, &ae) {
				t.Fatal("missing APIError")
			}
			dump := err.Error() + fmt.Sprintf("%#v", ae)
			// Decode fallback JSON just as a consumer of APIError.Body could.
			if tc.decodeJSON && ae.Body != "" {
				var v any
				if json.Unmarshal([]byte(ae.Body), &v) == nil {
					dump += fmt.Sprint(v)
				}
			}
			if tc.unfold {
				dump = strings.ReplaceAll(dump, "\r\n ", "")
				if _, suffix, ok := strings.Cut(ae.Body, "\r\n "); ok {
					if decoded, decodeErr := base64.StdEncoding.DecodeString(strings.ReplaceAll(suffix, "\r\n ", "")); decodeErr == nil {
						dump += string(decoded)
					}
				}
			}
			for _, s := range []string{token, basic, strings.TrimRight(basic, "="), transportToken} {
				if strings.Contains(dump, s) {
					t.Errorf("recoverable synthetic credential retained")
				}
			}
			// A folded Authorization field's continuation must not survive, even if
			// the first segment was already scrubbed.
			if tc.unfold && strings.Contains(ae.Body, "\r\n ") {
				t.Errorf("Authorization continuation retained: %q", ae.Body)
			}
		})
	}
}
