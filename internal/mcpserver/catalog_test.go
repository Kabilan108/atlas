package mcpserver_test

import (
	"context"
	"slices"
	"testing"

	"github.com/kabilan108/atlas/internal/mcpserver"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/catalog"
)

// Every op with the MCP surface is a tool, and nothing else is.
func TestToolsListMatchesCatalog(t *testing.T) {
	srv := startServer(t, mcpserver.Options{})
	result, err := connect(t, srv.url, nil).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var tools, want []string
	for _, tool := range result.Tools {
		tools = append(tools, tool.Name)
	}
	for _, def := range catalog.All() {
		if def.Info().Surfaces.Has(ops.MCP) {
			want = append(want, def.Info().Name)
		}
	}
	slices.Sort(tools)
	slices.Sort(want)
	if !slices.Equal(tools, want) {
		t.Fatalf("tools/list = %v, want the catalog's MCP ops %v", tools, want)
	}
}
