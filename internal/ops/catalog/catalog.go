// Package catalog lists every op, so each front end registers them from one
// place. Each domain package exports Ops(); add new domains here.
package catalog

import (
	"slices"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
	"github.com/kabilan108/atlas/internal/ops/snippet"
)

// All returns every op in a stable order.
func All() []ops.Def {
	return slices.Concat(
		pr.Ops(),
		snippet.Ops(),
	)
}
