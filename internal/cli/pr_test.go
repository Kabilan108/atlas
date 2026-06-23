package cli

import (
	"reflect"
	"strings"
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

func TestNormalizePRDescriptionMarkdownAddsBlankLinesBeforeLists(t *testing.T) {
	t.Parallel()

	body := "Description/Summary of Changes:\n- Add reusable CNS Events.xml annotation import logic.\n- Deduplicate imports by CNS event fingerprint.\n\nTo-Do:\n- [ ] Code review\n- [ ] Address feedback\n\nTesting Steps:\n1. Run `pytest ...`.\n"
	want := "Description/Summary of Changes:\n\n- Add reusable CNS Events.xml annotation import logic.\n- Deduplicate imports by CNS event fingerprint.\n\nTo-Do:\n\n- [ ] Code review\n- [ ] Address feedback\n\nTesting Steps:\n\n1. Run `pytest ...`.\n"

	if got := normalizePRDescriptionMarkdown(body); got != want {
		t.Fatalf("normalizePRDescriptionMarkdown() = %q, want %q", got, want)
	}
}

func TestNormalizePRDescriptionMarkdownPreservesExistingBlankLinesAndCodeFences(t *testing.T) {
	t.Parallel()

	body := "Description:\n\n- Already separated\n\n```md\nExample:\n- Not a real list block\n```\n\nChecklist:\n\t- Tab-indented task\n"
	want := "Description:\n\n- Already separated\n\n```md\nExample:\n- Not a real list block\n```\n\nChecklist:\n\n\t- Tab-indented task\n"

	if got := normalizePRDescriptionMarkdown(body); got != want {
		t.Fatalf("normalizePRDescriptionMarkdown() = %q, want %q", got, want)
	}
}

func TestNormalizePRDescriptionMarkdownPreservesCRLF(t *testing.T) {
	t.Parallel()

	body := "To-Do:\r\n- [ ] Review\r\n"
	want := "To-Do:\r\n\r\n- [ ] Review\r\n"

	if got := normalizePRDescriptionMarkdown(body); got != want {
		t.Fatalf("normalizePRDescriptionMarkdown() = %q, want %q", got, want)
	}
}

func TestNormalizePRDescriptionMarkdownPreservesLongFenceWithShortFenceInside(t *testing.T) {
	t.Parallel()

	body := "````md\n```md\nExample:\n- Not a real list block\n```\nStill fenced:\n- Also not a real list block\n````\nReal section:\n- Real list block\n"
	want := "````md\n```md\nExample:\n- Not a real list block\n```\nStill fenced:\n- Also not a real list block\n````\nReal section:\n\n- Real list block\n"

	if got := normalizePRDescriptionMarkdown(body); got != want {
		t.Fatalf("normalizePRDescriptionMarkdown() = %q, want %q", got, want)
	}
}

func TestNormalizePRDescriptionMarkdownAddsInlineCardsToStandaloneURLs(t *testing.T) {
	t.Parallel()

	body := "https://moberganalytics.atlassian.net/browse/MCP-7163\n\nhttps://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361"
	want := "[https://moberganalytics.atlassian.net/browse/MCP-7163](https://moberganalytics.atlassian.net/browse/MCP-7163){: data-inline-card='' }\n\n[https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361](https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361){: data-inline-card='' }"

	if got := normalizePRDescriptionMarkdown(body); got != want {
		t.Fatalf("normalizePRDescriptionMarkdown() = %q, want %q", got, want)
	}
}

func TestNormalizePRDescriptionMarkdownAddsInlineCardsToURLListItems(t *testing.T) {
	t.Parallel()

	body := "- https://moberganalytics.atlassian.net/browse/MCP-7163\n  1.   https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361  \n"
	want := "- [https://moberganalytics.atlassian.net/browse/MCP-7163](https://moberganalytics.atlassian.net/browse/MCP-7163){: data-inline-card='' }\n  1.   [https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361](https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361){: data-inline-card='' }  \n"

	if got := normalizePRDescriptionMarkdown(body); got != want {
		t.Fatalf("normalizePRDescriptionMarkdown() = %q, want %q", got, want)
	}
}

func TestNormalizePRDescriptionMarkdownSkipsInlineCardIneligibleURLs(t *testing.T) {
	t.Parallel()

	body := strings.Join([]string{
		"https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361{: data-inline-card='' }",
		"[https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361](https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361){: data-inline-card='' }",
		"[dashboard PR](https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361)",
		"See https://bitbucket.org/moberg-analytics/dashboard/pull-requests/1361",
		"https://example.com/moberg-analytics/dashboard/pull-requests/1361",
		"```md",
		"https://moberganalytics.atlassian.net/browse/MCP-7163",
		"```",
		"",
	}, "\n")

	if got := normalizePRDescriptionMarkdown(body); got != body {
		t.Fatalf("normalizePRDescriptionMarkdown() = %q, want %q", got, body)
	}
}

func TestBuildPRCreateInputNormalizesDescription(t *testing.T) {
	t.Parallel()

	input := buildPRCreateInput("Title", "Checklist:\n- Review\n", "feature", "main", nil, false)
	want := "Checklist:\n\n- Review\n"

	if input.Description != want {
		t.Fatalf("Description = %q, want %q", input.Description, want)
	}
	if input.Title != "Title" || input.Source.Branch.Name != "feature" || input.Destination.Branch.Name != "main" {
		t.Fatalf("buildPRCreateInput() returned unexpected refs: %#v", input)
	}
}

func TestBuildPRDescriptionUpdate(t *testing.T) {
	t.Parallel()

	if description, changed := buildPRDescriptionUpdate("Existing description", "", false, false); changed || description != nil {
		t.Fatalf("buildPRDescriptionUpdate() changed title-only edit: description=%#v changed=%v", description, changed)
	}

	description, changed := buildPRDescriptionUpdate("Existing description", "Checklist:\n- Review\n", true, false)
	if !changed || description == nil {
		t.Fatalf("buildPRDescriptionUpdate() changed = %v, description = %#v", changed, description)
	}
	want := "Checklist:\n\n- Review\n"
	if *description != want {
		t.Fatalf("description = %q, want %q", *description, want)
	}

	description, changed = buildPRDescriptionUpdate(want, "Checklist:\n- Review\n", true, false)
	if changed || description != nil {
		t.Fatalf("buildPRDescriptionUpdate() changed equivalent body: description=%#v changed=%v", description, changed)
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

func TestBuildCommentCreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec commentSpec
		want func(*testing.T, bitbucket.CommentCreate)
	}{
		{
			name: "top level",
			spec: commentSpec{Body: "Looks good"},
			want: func(t *testing.T, got bitbucket.CommentCreate) {
				t.Helper()
				if got.Content.Raw != "Looks good" {
					t.Fatalf("Content.Raw = %q, want %q", got.Content.Raw, "Looks good")
				}
				if got.Parent != nil || got.Inline != nil {
					t.Fatalf("Parent = %#v Inline = %#v, want nil", got.Parent, got.Inline)
				}
			},
		},
		{
			name: "reply",
			spec: commentSpec{Body: "Fixed", ReplyTo: 123},
			want: func(t *testing.T, got bitbucket.CommentCreate) {
				t.Helper()
				if got.Parent == nil || got.Parent.ID != 123 {
					t.Fatalf("Parent = %#v, want id 123", got.Parent)
				}
				if got.Inline != nil {
					t.Fatalf("Inline = %#v, want nil", got.Inline)
				}
			},
		},
		{
			name: "file level",
			spec: commentSpec{Body: "Question", Path: "internal/cli/pr.go"},
			want: func(t *testing.T, got bitbucket.CommentCreate) {
				t.Helper()
				if got.Inline == nil || got.Inline.Path != "internal/cli/pr.go" {
					t.Fatalf("Inline = %#v, want path", got.Inline)
				}
				if got.Inline.From != nil || got.Inline.To != nil {
					t.Fatalf("Inline = %#v, want no line", got.Inline)
				}
			},
		},
		{
			name: "new line",
			spec: commentSpec{Body: "Question", Path: "internal/cli/pr.go", Line: 42, Side: "new"},
			want: func(t *testing.T, got bitbucket.CommentCreate) {
				t.Helper()
				if got.Inline == nil || got.Inline.To == nil || *got.Inline.To != 42 {
					t.Fatalf("Inline = %#v, want to line 42", got.Inline)
				}
				if got.Inline.From != nil {
					t.Fatalf("From = %#v, want nil", got.Inline.From)
				}
			},
		},
		{
			name: "old line",
			spec: commentSpec{Body: "Question", Path: "internal/cli/pr.go", Line: 42, Side: "old"},
			want: func(t *testing.T, got bitbucket.CommentCreate) {
				t.Helper()
				if got.Inline == nil || got.Inline.From == nil || *got.Inline.From != 42 {
					t.Fatalf("Inline = %#v, want from line 42", got.Inline)
				}
				if got.Inline.To != nil {
					t.Fatalf("To = %#v, want nil", got.Inline.To)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := buildCommentCreate(tt.spec)
			if err != nil {
				t.Fatalf("buildCommentCreate() error = %v", err)
			}
			tt.want(t, got)
		})
	}
}

func TestBuildCommentCreateRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec commentSpec
	}{
		{name: "empty body", spec: commentSpec{Body: "  "}},
		{name: "line without path", spec: commentSpec{Body: "body", Line: 3}},
		{name: "side without line", spec: commentSpec{Body: "body", Path: "file.go", Side: "old"}},
		{name: "invalid side", spec: commentSpec{Body: "body", Path: "file.go", Line: 3, Side: "right"}},
		{name: "reply with path", spec: commentSpec{Body: "body", ReplyTo: 1, Path: "file.go"}},
		{name: "reply with line", spec: commentSpec{Body: "body", ReplyTo: 1, Line: 3}},
		{name: "reply with side", spec: commentSpec{Body: "body", ReplyTo: 1, Side: "new"}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := buildCommentCreate(tt.spec); err == nil {
				t.Fatal("buildCommentCreate() error = nil, want error")
			}
		})
	}
}

func TestBuildCommentCreatesRejectsInvalidBatchBeforePosting(t *testing.T) {
	t.Parallel()

	specs := []commentSpec{
		{Body: "first"},
		{Body: "second", Path: "file.go", Side: "old"},
	}

	if _, err := buildCommentCreates(specs, false); err == nil {
		t.Fatal("buildCommentCreates() error = nil, want error")
	}
}

func TestApplyAttribution(t *testing.T) {
	t.Parallel()

	body := "Looks good.\n"
	want := "Looks good.\n\n" + atlasAttributionFooter
	if got := applyAttribution(body, true); got != want {
		t.Fatalf("applyAttribution() = %q, want %q", got, want)
	}

	if got := applyAttribution(body, false); got != body {
		t.Fatalf("applyAttribution(disabled) = %q, want %q", got, body)
	}

	if got := applyAttribution(want, true); got != want {
		t.Fatalf("applyAttribution() duplicated footer: %q", got)
	}
}

func TestBuildPRCreateInputAppliesAttribution(t *testing.T) {
	t.Parallel()

	input := buildPRCreateInput("Title", "Checklist:\n- Review\n", "feature", "main", nil, true)
	want := "Checklist:\n\n- Review\n\n" + atlasAttributionFooter
	if input.Description != want {
		t.Fatalf("Description = %q, want %q", input.Description, want)
	}
}

func TestBuildCommentCreatesAppliesAttribution(t *testing.T) {
	t.Parallel()

	inputs, err := buildCommentCreates([]commentSpec{{Body: "first"}, {Body: "second"}}, true)
	if err != nil {
		t.Fatalf("buildCommentCreates() error = %v", err)
	}
	for index, input := range inputs {
		if !strings.Contains(input.Content.Raw, atlasAttributionMarker) {
			t.Fatalf("input %d missing attribution: %q", index, input.Content.Raw)
		}
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
