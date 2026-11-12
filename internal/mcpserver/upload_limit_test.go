package mcpserver_test

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/mcpserver"
	jiraops "github.com/kabilan108/atlas/internal/ops/jira"
)

func TestBodyCapFitsTheLargestAdvertisedUpload(t *testing.T) {
	encoded := base64.StdEncoding.EncodedLen(jiraops.MaxAttachBytes)
	if envelope := 1 << 20; mcpserver.DefaultMaxBodyBytes < encoded+envelope {
		t.Fatalf("DefaultMaxBodyBytes %d cannot carry %d bytes of base64 files plus a %d-byte envelope", mcpserver.DefaultMaxBodyBytes, encoded, envelope)
	}
}

// A jira_attach call at the op's own limits (20 files, MaxAttachBytes in
// all) reaches the tool and Jira instead of failing with 413.
func TestMaxSizeAttachIsNotRejected(t *testing.T) {
	var uploaded atomic.Int64
	jiraAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/attachments"):
			n, _ := io.Copy(io.Discard, r.Body)
			uploaded.Add(n)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `[{"id":"10001","filename":"f0.bin","size":1}]`)
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(jiraAPI.Close)
	handler, err := mcpserver.Handler(mcpserver.Options{JiraOptions: []jira.Option{jira.WithBaseURL(jiraAPI.URL)}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	per := jiraops.MaxAttachBytes / jiraops.MaxAttachFiles
	content := base64.StdEncoding.EncodeToString(make([]byte, per))
	files := make([]any, jiraops.MaxAttachFiles)
	for i := range files {
		files[i] = map[string]any{"filename": "f" + strconv.Itoa(i) + ".bin", "content": content}
	}
	header := credentials()
	header.Set(mcpserver.HeaderJiraToken, testJiraToken)
	text, isError := callText(t, connect(t, server.URL+"/mcp", header), "jira_attach", map[string]any{"issue": "CSM-1", "files": files})
	if isError {
		t.Fatalf("max-size attach failed: %s", text)
	}
	if got := uploaded.Load(); got < int64(jiraops.MaxAttachBytes) {
		t.Fatalf("Jira received %d bytes, want the %d-byte upload", got, jiraops.MaxAttachBytes)
	}
}
