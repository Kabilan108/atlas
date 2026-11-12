package pr

import (
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

// Side is the side of the diff an inline comment's line is on.
type Side string

const (
	SideNew Side = "new"
	SideOld Side = "old"
)

func (Side) Enum() []string { return []string{string(SideNew), string(SideOld)} }

// CommentSpec is one comment to post: top level, a reply, or inline on a
// file or line. Its JSON is the format of `atlas pr review --comment-spec`.
type CommentSpec struct {
	Body    string `json:"body" jsonschema:"markdown"`
	ReplyTo int    `json:"reply_to,omitempty" jsonschema:"comment ID to reply to"`
	Path    string `json:"path,omitempty" jsonschema:"file, for an inline comment"`
	Line    int    `json:"line,omitempty" jsonschema:"line number in the file's new version (old version with side old); take it from pr_diff with line_numbers"`
	Side    Side   `json:"side,omitempty" jsonschema:"side of line; default new"`
}

// build checks the spec and turns it into the Bitbucket request body.
func (s CommentSpec) build() (bitbucket.CommentCreate, error) {
	if strings.TrimSpace(s.Body) == "" {
		return bitbucket.CommentCreate{}, ops.Errorf(ops.Invalid, "comment body cannot be empty")
	}
	if s.ReplyTo < 0 {
		return bitbucket.CommentCreate{}, ops.Errorf(ops.Invalid, "reply_to must be greater than zero")
	}
	if s.Line < 0 {
		return bitbucket.CommentCreate{}, ops.Errorf(ops.Invalid, "line must be greater than zero")
	}
	if s.Side != "" && s.Side != SideNew && s.Side != SideOld {
		return bitbucket.CommentCreate{}, ops.Errorf(ops.Invalid, "invalid side %q: expected one of new, old", s.Side)
	}
	content := bitbucket.ContentInput{Raw: s.Body}
	path := strings.TrimSpace(s.Path)
	switch {
	case s.ReplyTo > 0 && (path != "" || s.Line != 0 || s.Side != ""):
		return bitbucket.CommentCreate{}, ops.Errorf(ops.Invalid, "reply_to cannot be combined with path, line, or side")
	case s.ReplyTo > 0:
		return bitbucket.CommentCreate{Content: content, Parent: &bitbucket.ParentInput{ID: s.ReplyTo}}, nil
	case s.Line > 0 && path == "":
		return bitbucket.CommentCreate{}, ops.Errorf(ops.Invalid, "line requires path")
	case s.Side != "" && path == "":
		return bitbucket.CommentCreate{}, ops.Errorf(ops.Invalid, "side requires path")
	case s.Side != "" && s.Line == 0:
		return bitbucket.CommentCreate{}, ops.Errorf(ops.Invalid, "side requires line")
	case path == "":
		return bitbucket.CommentCreate{Content: content}, nil
	}
	inline := bitbucket.InlineInput{Path: path}
	if s.Line > 0 {
		line := s.Line
		if s.Side == SideOld {
			inline.From = &line
		} else {
			inline.To = &line
		}
	}
	return bitbucket.CommentCreate{Content: content, Inline: &inline}, nil
}
