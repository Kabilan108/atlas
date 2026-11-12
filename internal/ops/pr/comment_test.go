package pr_test

import (
	"testing"

	"github.com/kabilan108/atlas/internal/ops/pr"
)

func TestComment(t *testing.T) {
	tests := []struct {
		name, args, body string
	}{
		{"top level", `"body":"hi"`, `{"content":{"raw":"hi"}}`},
		{"reply", `"body":"thanks","reply_to":101`, `{"content":{"raw":"thanks"},"parent":{"id":101}}`},
		{"new line", `"body":"x","path":"src/login.go","line":12`, `{"content":{"raw":"x"},"inline":{"path":"src/login.go","to":12}}`},
		{"old line", `"body":"x","path":"src/login.go","line":8,"side":"old"}`, `{"content":{"raw":"x"},"inline":{"path":"src/login.go","from":8}}`},
		{"file", `"body":"x","path":"README.md"`, `{"content":{"raw":"x"},"inline":{"path":"README.md"}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env, fixture := newTestEnv(t, "")
			args := `{"repo":"acme/widgets","pr":"42",` + tc.args
			if args[len(args)-1] != '}' {
				args += "}"
			}
			result, err := invoke(t, pr.Comment, env, args)
			if err != nil {
				t.Fatal(err)
			}
			if want := `{"id":201,"pull_request":42,"url":"https://bitbucket.org/acme/widgets/pull-requests/42#comment-201"}`; result.Text != want {
				t.Fatalf("receipt = %s", result.Text)
			}
			wantCalls(t, fixture, "GET "+prPath, "POST "+prPath+"/comments", "  "+tc.body)
		})
	}
}

func TestCommentEdit(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	result, err := invoke(t, pr.Comment, env, `{"repo":"acme/widgets","pr":"42","edit":101,"body":"Handle the error."}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"id":101,"pull_request":42,"edited":true,"url":"https://bitbucket.org/acme/widgets/pull-requests/42#comment-101"}`; result.Text != want {
		t.Fatalf("receipt = %s", result.Text)
	}
	wantCalls(t, fixture, "GET "+prPath, "PUT "+prPath+"/comments/101", `  {"content":{"raw":"Handle the error."}}`)
}

func TestCommentRejects(t *testing.T) {
	for _, tc := range []struct{ args, want string }{
		{`"body":" "`, "body cannot be empty"},
		{`"body":"x","reply_to":1,"path":"a.go"`, "reply_to cannot be combined"},
		{`"body":"x","line":3`, "line requires path"},
		{`"body":"x","path":"a.go","side":"old"`, "side requires line"},
		{`"body":"x","path":"a.go","line":1,"side":"left"`, "side"},
		{`"body":"x","edit":5,"path":"a.go"`, "edit replaces only the body"},
		{`"body":"","edit":5`, "body cannot be empty"},
	} {
		env, fixture := newTestEnv(t, "")
		_, err := invoke(t, pr.Comment, env, `{"repo":"acme/widgets","pr":"42",`+tc.args+`}`)
		wantInvalid(t, err, fixture, tc.want)
	}
}

func TestCommentDelete(t *testing.T) {
	env, fixture := newTestEnv(t, "")
	result, err := invoke(t, pr.CommentDelete, env, `{"repo":"acme/widgets","pr":"42","comment":101}`)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"pull_request":42,"comment":101,"deleted":true}`; result.Text != want {
		t.Fatalf("receipt = %s", result.Text)
	}
	wantCalls(t, fixture, "GET "+prPath, "DELETE "+prPath+"/comments/101")
	if !pr.CommentDelete.Destructive || !pr.Comment.Destructive {
		t.Fatal("pr_comment_delete and pr_comment (whose edit replaces a body) are destructive")
	}

	env, fixture = newTestEnv(t, "")
	_, err = invoke(t, pr.CommentDelete, env, `{"repo":"acme/widgets","pr":"42"}`)
	wantInvalid(t, err, fixture, "comment is required")
}
