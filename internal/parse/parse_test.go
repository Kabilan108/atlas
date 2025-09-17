package parse

import "testing"

func TestConfluencePageID(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "plain id", input: "12345", want: "12345"},
		{name: "space page path", input: "https://example.atlassian.net/wiki/spaces/ENG/pages/67890/Spec", want: "67890"},
		{name: "wiki page path", input: "https://example.atlassian.net/wiki/pages/13579/Page", want: "13579"},
		{name: "render action", input: "https://example.atlassian.net/wiki/renderpagecontent.action?pageId=24680", want: "24680"},
		{name: "fragment page id", input: "https://example.atlassian.net/wiki/spaces/ENG/pages/viewpage.action#pageId=998877", want: "998877"},
		{name: "invalid", input: "https://example.atlassian.net/wiki/display/ENG/Home", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ConfluencePageID(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got none with %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestParsePullRequestRef(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    PullRequestRef
		wantErr bool
	}{
		{
			name:  "short form",
			input: "workspace/repo#42",
			want:  PullRequestRef{Workspace: "workspace", RepoSlug: "repo", ID: 42},
		},
		{
			name:  "ui url",
			input: "https://bitbucket.org/workspace/repo/pull-requests/77",
			want:  PullRequestRef{Workspace: "workspace", RepoSlug: "repo", ID: 77},
		},
		{
			name:  "api url",
			input: "https://api.bitbucket.org/2.0/repositories/workspace/repo/pullrequests/101",
			want:  PullRequestRef{Workspace: "workspace", RepoSlug: "repo", ID: 101},
		},
		{
			name:    "invalid",
			input:   "https://bitbucket.org/workspace/repo/pull-requests/not-a-number",
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParsePullRequestRef(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got ref: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %+v, got %+v", tc.want, got)
			}
		})
	}
}
