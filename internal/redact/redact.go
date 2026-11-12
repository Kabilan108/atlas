// Package redact removes an API client's own credentials from text a server
// sent back, such as an error body that echoes the request. Both product
// clients run every error body through it before the text can reach a
// terminal, a log, or an MCP result.
package redact

import (
	"cmp"
	"encoding/base64"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Mask replaces each credential.
const Mask = "[REDACTED]"

// authorizationValue matches an Authorization label's value, including
// folded continuation lines that begin with a space or tab.
var authorizationValue = regexp.MustCompile(`(?i)(authorization"?\s*[:=]\s*"?)[^\r\n"]*(?:\r?\n[ \t][^\r\n"]*)*`)

// Credentials redacts the encodings a Basic-auth client sends its user and
// token in. The zero value redacts only Authorization labels.
type Credentials struct {
	// secrets are matched exactly; email case-insensitively.
	secrets []string
	email   *regexp.Regexp
}

// Basic returns a redactor for a client authenticating as user with token:
// the token raw and URL-escaped, the Basic value in standard and URL-safe
// base64 with and without padding, and user when it is an email address.
// A plain username is left alone because it usually doubles as the name of
// a workspace that error messages legitimately mention.
func Basic(user, token string) *Credentials {
	c := &Credentials{}
	add := func(values ...string) {
		for _, v := range values {
			if v != "" && !slices.Contains(c.secrets, v) {
				c.secrets = append(c.secrets, v)
			}
		}
	}
	if token != "" {
		pair := []byte(user + ":" + token)
		for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.URLEncoding} {
			encoded := enc.EncodeToString(pair)
			add(encoded, strings.TrimRight(encoded, "="))
		}
		add(token, url.QueryEscape(token), url.PathEscape(token), strings.ReplaceAll(token, "/", `\/`))
	}
	if strings.Contains(user, "@") {
		quoted := []string{regexp.QuoteMeta(user)}
		if escaped := url.QueryEscape(user); escaped != user {
			quoted = append(quoted, regexp.QuoteMeta(escaped))
		}
		c.email = regexp.MustCompile(`(?i)` + strings.Join(quoted, "|"))
		add(strings.ToLower(user))
	}
	// Longest first, so a token inside the Basic value never leaves part of
	// the encoding behind.
	slices.SortStableFunc(c.secrets, func(a, b string) int { return cmp.Compare(len(b), len(a)) })
	return c
}

// String returns s without the credentials or the value of anything
// labelled Authorization. If a credential can still be spelled once
// whitespace is removed and JSON or percent escapes are decoded (a wrapped
// base64 value, say), the whole string is withheld.
func (c *Credentials) String(s string) string {
	if c == nil {
		return authorizationValue.ReplaceAllString(s, "${1}"+Mask)
	}
	for _, secret := range c.secrets {
		s = strings.ReplaceAll(s, secret, Mask)
	}
	if c.email != nil {
		s = c.email.ReplaceAllString(s, Mask)
	}
	s = authorizationValue.ReplaceAllString(s, "${1}"+Mask)
	if c.spells(s) {
		return Mask
	}
	return s
}

func (c *Credentials) spells(s string) bool {
	compact := strings.Join(strings.Fields(s), "")
	forms := []string{compact, strings.ToLower(compact), unescapeJSON(compact)}
	if decoded, err := url.PathUnescape(compact); err == nil {
		forms = append(forms, decoded)
	}
	for _, form := range forms {
		for _, secret := range c.secrets {
			if strings.Contains(form, secret) {
				return true
			}
		}
	}
	return false
}

// unescapeJSON decodes the \uXXXX and single-character escapes a JSON
// string can use, leaving anything else as is.
func unescapeJSON(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		switch next := s[i+1]; next {
		case 'u':
			if i+6 <= len(s) {
				if r, err := strconv.ParseUint(s[i+2:i+6], 16, 32); err == nil {
					b.WriteRune(rune(r))
					i += 5
					continue
				}
			}
			b.WriteByte(s[i])
		case '"', '\\', '/':
			b.WriteByte(next)
			i++
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// Error returns err with the credentials removed from its text, for
// transport and URL errors that can carry a redirect target or whatever a
// custom transport put there. Unwrap keeps the chain, so timeouts and HTTP
// statuses still classify.
func (c *Credentials) Error(err error) error {
	if err == nil {
		return nil
	}
	message := c.String(err.Error())
	if message == err.Error() {
		return err
	}
	return &redactedError{message: message, err: err}
}

type redactedError struct {
	message string
	err     error
}

func (e *redactedError) Error() string { return e.message }
func (e *redactedError) Unwrap() error { return e.err }
