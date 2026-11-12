package jira

import (
	"context"
	"encoding/base64"
	"runtime"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/jira/jiratest"
)

// Files that together pass the per-call limit are refused from their
// base64 lengths, before any of them is decoded.
func TestAttachChecksSizesBeforeDecoding(t *testing.T) {
	content := base64.StdEncoding.EncodeToString(make([]byte, 6<<20))
	in := AttachInput{IssueRef: IssueRef{Issue: "CSM-756"}, Files: []AttachFile{
		{Filename: "a.bin", Content: content}, {Filename: "b.bin", Content: content}}}
	fake := jiratest.New()
	env := newEnv(t, fake)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := attach(context.Background(), env, in)
	runtime.ReadMemStats(&after)
	wantKind(t, err, ops.Invalid, "over the 10485760-byte limit")
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 1<<20 {
		t.Errorf("allocated %d bytes before refusing %d bytes of files", grew, 12<<20)
	}
	noWrites(t, fake)
}

func TestDecodeBase64(t *testing.T) {
	for in, want := range map[string]string{
		"":                   "",
		"YQ==":               "a",
		"YWI=":               "ab",
		"YWJj":               "abc",
		" YW\tJj\r\nZA== \n": "abcd",
	} {
		size, err := decodedSize(in)
		if err != nil || size != len(want) {
			t.Errorf("decodedSize(%q) = %d, %v; want %d", in, size, err, len(want))
			continue
		}
		got, err := decodeBase64(in, size)
		if err != nil || string(got) != want {
			t.Errorf("decodeBase64(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"YQ=", "YQ==YQ==", "Y===", "abc"} {
		if size, err := decodedSize(bad); err == nil {
			t.Errorf("decodedSize(%q) = %d, want an error", bad, size)
		}
	}
	if _, err := decodeBase64("!!!!", 3); err == nil {
		t.Error("decodeBase64 accepted non-base64 characters")
	}
}
