package redact_test

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/redact"
)

func TestBasicRedactsEveryEncoding(t *testing.T) {
	const user, token = "Ada.L@example.invalid", "AbC/def+ghi=0123456789"
	basic := base64.StdEncoding.EncodeToString([]byte(user + ":" + token))
	urlSafe := base64.URLEncoding.EncodeToString([]byte(user + ":" + token))
	r := redact.Basic(user, token)
	for name, input := range map[string]string{
		"token":            "x " + token + " y",
		"query escaped":    "AbC%2Fdef%2Bghi%3D0123456789",
		"basic":            "Basic " + basic,
		"basic unpadded":   strings.TrimRight(basic, "="),
		"url-safe":         strings.TrimRight(urlSafe, "="),
		"email upper":      strings.ToUpper(user),
		"email escaped":    "ada.l%40example.invalid",
		"slash escaped":    `AbC\/def+ghi=0123456789`,
		"folded":           "Authorization: Bearer\r\n other-credential",
		"wrapped":          basic[:10] + "\n  " + basic[10:],
		"unicode escaped":  `AbC/def+ghi=0123456789`,
		"percent spelled":  "%41bC/def+ghi=0123456789",
		"label other form": `{"authorization":"Bearer other-credential"}`,
	} {
		got := r.String(input)
		for _, secret := range []string{token, basic, strings.TrimRight(urlSafe, "="), "ada.l@example.invalid", "other-credential"} {
			if strings.Contains(strings.ToLower(got), strings.ToLower(secret)) {
				t.Errorf("%s: %q still holds %q", name, got, secret)
			}
		}
	}
}

func TestBasicKeepsPlainUsernames(t *testing.T) {
	r := redact.Basic("acme", "a-long-api-token")
	if got := r.String("repository acme/widgets not found"); got != "repository acme/widgets not found" {
		t.Fatalf("plain username redacted: %q", got)
	}
}

func TestErrorKeepsTheChain(t *testing.T) {
	r := redact.Basic("ada@example.invalid", "a-long-api-token")
	err := r.Error(fmt.Errorf("proxy said a-long-api-token: %w", os.ErrDeadlineExceeded))
	if strings.Contains(err.Error(), "a-long-api-token") || !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("err = %v, chain kept = %v", err, errors.Is(err, os.ErrDeadlineExceeded))
	}
	plain := errors.New("nothing secret")
	if r.Error(plain) != plain {
		t.Fatal("an error without credentials was rewrapped")
	}
}
