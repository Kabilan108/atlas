package git

import (
	"os"
	"testing"
)

// TestMain clears git's repository variables. Tests here run git in temp
// directories; under "git rebase --exec" GIT_DIR points at the real repo, so
// their commits would land there.
func TestMain(m *testing.M) {
	for _, name := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_NAMESPACE", "GIT_PREFIX"} {
		os.Unsetenv(name)
	}
	os.Exit(m.Run())
}
