package mcpserver_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/mcpserver"
	"github.com/kabilan108/atlas/internal/ops"
)

type floodInput struct{}

// truncationNotes allows for the notes capResult appends when it cuts.
const truncationNotes = 512

func TestErrorResultsStayWithinTheResultCap(t *testing.T) {
	huge := strings.Repeat("e", 300<<10)
	partial := strings.Repeat("p", 300<<10)
	flood := &ops.Op[floodInput, string]{
		Meta: ops.Meta{Name: "flood", Description: "Fails loudly.", Surfaces: ops.MCP, ReadOnly: true},
		Run: func(context.Context, ops.Env, floodInput) (string, error) {
			return "", ops.Errorf(ops.Upstream, "%s", huge)
		},
		Markdown: func(string) (string, error) { return "", nil },
	}
	partialFlood := &ops.Op[floodInput, string]{
		Meta: ops.Meta{Name: "partial_flood", Description: "Fails loudly with output.", Surfaces: ops.MCP, ReadOnly: true},
		Run: func(context.Context, ops.Env, floodInput) (string, error) {
			return partial, ops.Errorf(ops.Partial, "%s", huge)
		},
		Markdown: func(out string) (string, error) { return out, nil },
	}
	session := connectInMemory(t, mcpserver.Options{}, flood, partialFlood)
	for _, tc := range []struct{ tool, kind, keep string }{
		{"flood", "upstream: ", ""},
		{"partial_flood", "partial: ", "pppp"},
	} {
		text, isError := callText(t, session, tc.tool, nil)
		if !isError {
			t.Fatalf("%s: not a tool error", tc.tool)
		}
		if len(text) > mcpserver.DefaultMaxResultBytes+truncationNotes {
			t.Errorf("%s: %d bytes of tool text, over the %d-byte cap", tc.tool, len(text), mcpserver.DefaultMaxResultBytes)
		}
		if !strings.HasPrefix(text, tc.kind) || !strings.Contains(text, "[truncated: ") || !strings.Contains(text, tc.keep) {
			t.Errorf("%s: lost the kind, the note, or the partial output: %q...", tc.tool, text[:min(len(text), 80)])
		}
	}
}

func TestUpstreamErrorBodiesStayWithinTheResultCap(t *testing.T) {
	var seen []string
	api := statusServer(t, http.StatusBadRequest, `{"errorMessages":["`+strings.Repeat("x", 300<<10)+`"]}`, &seen)
	url := serveDefs(t, mcpserver.Options{JiraOptions: []jira.Option{jira.WithBaseURL(api.URL)}}, jiraWhoami)
	header := credentials()
	header.Set(mcpserver.HeaderJiraToken, testJiraToken)
	text, isError := callText(t, connect(t, url, header), "jira_whoami_test", nil)
	if !isError || len(text) > mcpserver.DefaultMaxResultBytes+truncationNotes || !strings.HasPrefix(text, "invalid: ") {
		t.Fatalf("isError=%v, %d bytes, prefix %q", isError, len(text), text[:min(len(text), 40)])
	}
}
