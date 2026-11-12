package snippet

import "github.com/kabilan108/atlas/internal/ops"

// Ops returns the snippet ops in a stable order.
func Ops() []ops.Def {
	return []ops.Def{
		List,
		View,
		Create,
		Edit,
		Rename,
		Delete,
	}
}
