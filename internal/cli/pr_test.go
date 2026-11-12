package cli

import (
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

func TestStructuredDiffFlagHasShortAlias(t *testing.T) {
	t.Parallel()

	cmd := newPRDiffCmd()
	flag := cmd.Flags().Lookup("structured")
	if flag == nil {
		t.Fatal("structured flag is missing")
	}
	if flag.Shorthand != "s" {
		t.Fatalf("structured shorthand = %q, want %q", flag.Shorthand, "s")
	}
}

func TestViewRawFlagHasShortAlias(t *testing.T) {
	t.Parallel()

	cmd := newPRViewCmd()
	flag := cmd.Flags().Lookup("raw")
	if flag == nil {
		t.Fatal("raw flag is missing")
	}
	if flag.Shorthand != "r" {
		t.Fatalf("raw shorthand = %q, want %q", flag.Shorthand, "r")
	}
}

func TestShouldEditBodyInEditor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		title           string
		body            string
		bodyFile        string
		addReviewers    string
		removeReviewers string
		want            bool
	}{
		{name: "no explicit edits opens editor", want: true},
		{name: "title only does not open editor", title: "new title", want: false},
		{name: "add reviewer only does not open editor", addReviewers: "alice", want: false},
		{name: "remove reviewer only does not open editor", removeReviewers: "alice", want: false},
		{name: "body flag does not open editor", body: "body", want: false},
		{name: "body file does not open editor", bodyFile: "body.md", want: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := shouldEditBodyInEditor(tt.title, tt.body, tt.bodyFile, tt.addReviewers, tt.removeReviewers)
			if got != tt.want {
				t.Fatalf("shouldEditBodyInEditor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReviewerSummary(t *testing.T) {
	t.Parallel()

	alice := bitbucket.User{UUID: "{alice}", Username: "alice"}
	bob := bitbucket.User{UUID: "{bob}", Username: "bob"}
	pr := bitbucket.PullRequest{
		Reviewers: []bitbucket.User{alice, bob},
		Participants: []bitbucket.Participant{
			{User: alice, Role: "REVIEWER", Approved: true},
		},
	}

	want := "1 approved, 1 pending"
	if got := reviewerSummary(pr); got != want {
		t.Fatalf("reviewerSummary() = %q, want %q", got, want)
	}
}

func TestPRWriteCommandsRegistered(t *testing.T) {
	t.Parallel()

	cmd := newPRCmd()
	for _, name := range []string{"comment", "approve", "unapprove", "request-changes", "clear-change-request", "review"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			found, _, err := cmd.Find([]string{name})
			if err != nil {
				t.Fatalf("Find(%q) error = %v", name, err)
			}
			if found.Name() != name {
				t.Fatalf("Find(%q) = %q, want %q", name, found.Name(), name)
			}
		})
	}
}

func TestValidateCommentBodySources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		flags    commentBodyFlags
		required bool
		wantErr  bool
	}{
		{name: "required with body", flags: commentBodyFlags{body: "body"}, required: true},
		{name: "required missing", required: true, wantErr: true},
		{name: "optional missing", required: false},
		{name: "body and file", flags: commentBodyFlags{body: "body", bodyFile: "body.md"}, required: true, wantErr: true},
		{name: "body and editor", flags: commentBodyFlags{body: "body", editor: true}, required: true, wantErr: true},
		{name: "file and editor", flags: commentBodyFlags{bodyFile: "body.md", editor: true}, required: true, wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateCommentBodySources(tt.flags, tt.required)
			if tt.wantErr && err == nil {
				t.Fatal("validateCommentBodySources() error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("validateCommentBodySources() error = %v", err)
			}
		})
	}
}

func TestReviewActionFromFlags(t *testing.T) {
	t.Parallel()

	cmd := newPRReviewCmd()
	if err := cmd.Flags().Set("approve", "true"); err != nil {
		t.Fatal(err)
	}
	got, err := reviewActionFromFlags(cmd)
	if err != nil {
		t.Fatalf("reviewActionFromFlags() error = %v", err)
	}
	if got != "approve" {
		t.Fatalf("reviewActionFromFlags() = %q, want approve", got)
	}
}

func TestReviewActionFromFlagsRejectsMissingOrMultiple(t *testing.T) {
	t.Parallel()

	missing := newPRReviewCmd()
	if _, err := reviewActionFromFlags(missing); err == nil {
		t.Fatal("reviewActionFromFlags() missing error = nil, want error")
	}

	multiple := newPRReviewCmd()
	if err := multiple.Flags().Set("approve", "true"); err != nil {
		t.Fatal(err)
	}
	if err := multiple.Flags().Set("request-changes", "true"); err != nil {
		t.Fatal(err)
	}
	if _, err := reviewActionFromFlags(multiple); err == nil {
		t.Fatal("reviewActionFromFlags() multiple error = nil, want error")
	}
}
