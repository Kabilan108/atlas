package pr

import "github.com/kabilan108/atlas/internal/ops"

// Ops returns the pull request ops in a stable order.
func Ops() []ops.Def {
	return []ops.Def{
		View,
		List,
		Diff,
		Status,
		Create,
		Edit,
		Close,
		Comment,
		CommentDelete,
		Review,
		Approve,
		Unapprove,
		RequestChanges,
		ClearChangeRequest,
		Checks,
		Merge,
	}
}
