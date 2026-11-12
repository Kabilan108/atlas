package mcpserver

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCapResultKeepsShortText(t *testing.T) {
	if got := capResult("hello", 10); got != "hello" {
		t.Fatalf("capResult = %q", got)
	}
}

func TestCapResultCutsOnRuneBoundaryAndSaysSo(t *testing.T) {
	text := strings.Repeat("é", 100) // 200 bytes
	got := capResult(text, 51)
	body, note, ok := strings.Cut(got, "\n\n[truncated: ")
	if !ok {
		t.Fatalf("no truncation note in %q", got)
	}
	if !utf8.ValidString(body) || len(body) != 50 {
		t.Fatalf("body is %d bytes, valid=%v", len(body), utf8.ValidString(body))
	}
	if !strings.HasPrefix(note, "150 of 200 bytes omitted") {
		t.Fatalf("note = %q", note)
	}
}
