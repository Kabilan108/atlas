package ops_test

import (
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
)

func TestPartialOf(t *testing.T) {
	if err := ops.PartialOf(0, 3, "worklogs"); err != nil {
		t.Fatalf("no failures: err = %v", err)
	}
	for _, failed := range []int{1, 3} {
		err := ops.PartialOf(failed, 3, "worklogs")
		if ops.KindOf(err) != ops.Partial {
			t.Fatalf("failed=%d: kind = %v", failed, ops.KindOf(err))
		}
	}
}
