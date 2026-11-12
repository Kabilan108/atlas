package catalog_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/catalog"
	"github.com/kabilan108/atlas/internal/ops/opstest"
)

// Renamed inputs fail loudly under their old names instead of being ignored.
func TestOldInputNamesAreRejected(t *testing.T) {
	defs := map[string]ops.Def{}
	for _, def := range catalog.All() {
		defs[def.Info().Name] = def
	}
	for tool, args := range map[string]string{
		"pr_close":      `{"repo":"acme/widgets","pr":"1","comment":"bye"}`,
		"repo_list":     `{"name":"app"}`,
		"branch_list":   `{"repo":"acme/widgets","filter":"main"}`,
		"pipeline_list": `{"repo":"acme/widgets","pattern":"db-backup"}`,
		"jira_search":   `{"jql":"project = CSM","fields":["Product"]}`,
	} {
		def, ok := defs[tool]
		if !ok {
			t.Fatalf("no tool %s", tool)
		}
		_, err := def.Invoke(context.Background(), opstest.New("acme"), json.RawMessage(args))
		if ops.KindOf(err) != ops.Invalid || !strings.Contains(err.Error(), "unknown field") {
			t.Errorf("%s %s: err = %v, want an unknown field error", tool, args, err)
		}
	}
	for _, old := range []string{"jira_transitions"} {
		if _, ok := defs[old]; ok {
			t.Errorf("%s is still a tool", old)
		}
	}
}
