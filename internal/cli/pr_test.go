package cli

import (
	"reflect"
	"testing"

	"github.com/kabilan108/atlas/internal/bitbucket"
)

func TestParsePRRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		selector string
		want     prRef
	}{
		{name: "number", selector: "123", want: prRef{id: 123}},
		{name: "hash number", selector: "#123", want: prRef{id: 123}},
		{name: "branch", selector: "feature/auth", want: prRef{branch: "feature/auth"}},
		{
			name:     "bitbucket url",
			selector: "https://bitbucket.org/team/repo/pull-requests/42/fix-auth",
			want:     prRef{workspace: "team", repo: "repo", id: 42},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parsePRRef(tt.selector)
			if err != nil {
				t.Fatalf("parsePRRef() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("parsePRRef() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDiffFileNames(t *testing.T) {
	t.Parallel()

	diff := []byte(`diff --git a/README.md b/README.md
diff --git a/deleted.go /dev/null
diff --git a/internal/cli/pr.go b/internal/cli/pr.go
diff --git a/README.md b/README.md
`)

	want := []string{"README.md", "deleted.go", "internal/cli/pr.go"}
	if got := diffFileNames(diff); !reflect.DeepEqual(got, want) {
		t.Fatalf("diffFileNames() = %#v, want %#v", got, want)
	}
}

func TestNormalizePRState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state string
		want  string
	}{
		{state: "", want: "OPEN"},
		{state: "open", want: "OPEN"},
		{state: "merged", want: "MERGED"},
		{state: "closed", want: "DECLINED"},
		{state: "declined", want: "DECLINED"},
		{state: "superseded", want: "SUPERSEDED"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.state, func(t *testing.T) {
			t.Parallel()
			got, err := normalizePRState(tt.state)
			if err != nil {
				t.Fatalf("normalizePRState() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("normalizePRState() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizePRStateRejectsUnknown(t *testing.T) {
	t.Parallel()

	if _, err := normalizePRState("done"); err == nil {
		t.Fatal("normalizePRState() error = nil, want error")
	}
}

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
