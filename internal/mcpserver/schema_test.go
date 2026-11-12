package mcpserver_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/mcpserver"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type colour string

func (colour) Enum() []string { return []string{"red", "green"} }

type paintInput struct {
	Colour colour   `json:"colour"`
	Layers []colour `json:"layers,omitempty"`
}

// connectInMemory serves defs over an in-memory transport.
func connectInMemory(t *testing.T, opts mcpserver.Options, defs ...ops.Def) *mcp.ClientSession {
	t.Helper()
	server, err := mcpserver.NewServer(opts, defs)
	if err != nil {
		t.Fatal(err)
	}
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { serverSession.Close() })
	client := mcp.NewClient(&mcp.Implementation{Name: "atlas-test", Version: "test"}, nil)
	session, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func TestToolSchemaComesFromOps(t *testing.T) {
	paint := &ops.Op[paintInput, paintInput]{
		Meta: ops.Meta{Name: "paint", Description: "Paints.", Surfaces: ops.MCP},
		Run:  func(_ context.Context, _ ops.Env, in paintInput) (paintInput, error) { return in, nil },
	}
	session := connectInMemory(t, mcpserver.Options{}, paint)
	list, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(list.Tools[0].InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"additionalProperties":false,"properties":{"colour":{"enum":["red","green"],"type":"string"},"layers":{"items":{"enum":["red","green"],"type":"string"},"type":"array"}},"required":["colour"],"type":"object"}`
	if string(data) != want {
		t.Fatalf("schema = %s\nwant      %s", data, want)
	}
	if text, isError := callText(t, session, "paint", map[string]any{"colour": "blue"}); !isError || !strings.Contains(text, "invalid") {
		t.Fatalf("bad enum: isError=%v text=%q", isError, text)
	}
	if text, isError := callText(t, session, "paint", map[string]any{"colour": "red", "layers": []string{"green"}}); isError {
		t.Fatalf("valid call failed: %s", text)
	}
}
