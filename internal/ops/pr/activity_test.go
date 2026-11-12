package pr_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/kabilan108/atlas/internal/ops/pr/prtest"
)

func TestViewActivity(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	result, err := invoke(t, pr.View, env, `{"repo":"acme/widgets","pr":"42","activity":true}`)
	if err != nil {
		t.Fatal(err)
	}
	want := "\n## Activity\n\n" +
		"- 2025-03-05 16:00 UTC · Grace Hopper approved\n" +
		"- 2025-03-05 15:00 UTC · Ada Lovelace pushed abc1234\n" +
		"- 2025-03-03 08:05 UTC · Linus Torvalds requested changes\n" +
		"- 2025-03-03 08:00 UTC · Linus Torvalds commented (#103)\n" +
		"- 2025-03-02 09:00 UTC · Ada Lovelace changed title\n" +
		"- 2025-03-01 09:00 UTC · Ada Lovelace opened the pull request (feature/login → main at 9990001)\n"
	if !strings.HasPrefix(result.Text, prtest.Golden("view.md")) || !strings.HasSuffix(result.Text, want) {
		t.Fatalf("text:\n%s", result.Text)
	}
	if got := calls(fixture); !slices.Contains(got, "GET "+prPath+"/activity?pagelen=50") {
		t.Fatalf("requests = %v", got)
	}
}
