package cli

import (
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/catalog"
	"github.com/spf13/cobra"
)

// localOnlyCommands work on this machine (credentials, config, the binary,
// git, the server itself) and have no MCP counterpart by design.
var localOnlyCommands = map[string]string{
	"atlas login":         "stores credentials on this machine",
	"atlas config get":    "reads local config",
	"atlas config set":    "writes local config",
	"atlas config verify": "checks locally stored credentials",
	"atlas update":        "replaces the local binary",
	"atlas pr checkout":   "checks out a branch in the local repository",
	"atlas snippet clone": "clones into a local directory",
	"atlas mcp serve":     "runs the MCP server",
	"atlas help":          "cobra built-in",
}

// legacyCommands are not yet moved onto ops. This list only shrinks: each
// slice that binds one of these to an op removes it here, and nothing new may
// be added. Once it is empty, delete it.
var legacyCommands = map[string]bool{}

func leafCommands(cmd *cobra.Command) []*cobra.Command {
	if !cmd.HasSubCommands() {
		return []*cobra.Command{cmd}
	}
	var leaves []*cobra.Command
	for _, child := range cmd.Commands() {
		leaves = append(leaves, leafCommands(child)...)
	}
	return leaves
}

// MCP is the preferred interface, so every CLI command either runs an op
// (and so exists on MCP when the op does) or is local to this machine.
func TestEveryCLICommandIsBoundOrLocal(t *testing.T) {
	root := NewRootCmd("test")
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()

	defs := map[string]ops.Def{}
	for _, def := range catalog.All() {
		defs[def.Info().Name] = def
	}
	bound := map[string]string{}
	seen := map[string]bool{}
	for _, cmd := range leafCommands(root) {
		path := cmd.CommandPath()
		seen[path] = true
		opName := cmd.Annotations[opAnnotation]
		_, local := localOnlyCommands[path]
		local = local || strings.HasPrefix(path, "atlas completion ")
		switch {
		case opName != "" && (local || legacyCommands[path]):
			t.Errorf("%s is bound to %s; remove it from the allowlist", path, opName)
		case opName != "":
			def, ok := defs[opName]
			if !ok {
				t.Errorf("%s is bound to %s, which is not in the catalog", path, opName)
				continue
			}
			if !def.Info().Surfaces.Has(ops.CLI) {
				t.Errorf("%s is bound to %s, which lacks the CLI surface", path, opName)
			}
			if !def.Info().Surfaces.Has(ops.MCP) {
				t.Errorf("%s is bound to %s, which lacks the MCP surface: every CLI action must also be an MCP tool", path, opName)
			}
			if other, dup := bound[opName]; dup {
				t.Errorf("%s and %s are both bound to %s", other, path, opName)
			}
			bound[opName] = path
		case !local && !legacyCommands[path]:
			t.Errorf("%s runs no op and is not local-only: bind it to an op (MCP must have everything the CLI has)", path)
		}
	}
	for name, def := range defs {
		if def.Info().Surfaces.Has(ops.CLI) && bound[name] == "" {
			t.Errorf("op %s has the CLI surface but no command is bound to it", name)
		}
	}
	for path := range localOnlyCommands {
		if !seen[path] {
			t.Errorf("localOnlyCommands lists %s, which does not exist", path)
		}
	}
	for path := range legacyCommands {
		if !seen[path] {
			t.Errorf("legacyCommands lists %s, which does not exist; remove it", path)
		}
	}
}
