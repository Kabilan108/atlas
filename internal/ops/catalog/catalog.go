// Package catalog lists every op, so each front end registers them from one
// place.
package catalog

import (
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/pr"
)

// All returns every op in a stable order.
func All() []ops.Def {
	return []ops.Def{
		pr.View,
	}
}
