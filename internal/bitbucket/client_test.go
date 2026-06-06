package bitbucket

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHasReviewerMatchesStableIdentifiersOnly(t *testing.T) {
	t.Parallel()

	pr := PullRequest{
		Reviewers: []User{
			{
				Username:    "jdoe",
				Nickname:    "Jane Doe",
				DisplayName: "Jane Doe",
				AccountID:   "acct-123",
			},
		},
		Participants: []Participant{
			{
				Role: "REVIEWER",
				User: User{
					UUID:        "{reviewer-123}",
					DisplayName: "Jane Doe",
				},
			},
		},
	}

	for _, reviewer := range []string{"jdoe", "@jdoe", "acct-123", "reviewer-123"} {
		if !hasReviewer(pr, reviewer) {
			t.Fatalf("hasReviewer(%q) = false, want true", reviewer)
		}
	}

	for _, reviewer := range []string{"Jane Doe", "@Jane Doe"} {
		if hasReviewer(pr, reviewer) {
			t.Fatalf("hasReviewer(%q) = true, want false", reviewer)
		}
	}
}

func TestCreatePullRequestCommentSendsStructuredPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input CommentCreate
		check func(*testing.T, CommentCreate)
	}{
		{
			name:  "top level",
			input: CommentCreate{Content: ContentInput{Raw: "Looks good"}},
			check: func(t *testing.T, got CommentCreate) {
				t.Helper()
				if got.Content.Raw != "Looks good" {
					t.Fatalf("Content.Raw = %q, want Looks good", got.Content.Raw)
				}
				if got.Parent != nil || got.Inline != nil {
					t.Fatalf("Parent = %#v Inline = %#v, want nil", got.Parent, got.Inline)
				}
			},
		},
		{
			name:  "reply",
			input: CommentCreate{Content: ContentInput{Raw: "Fixed"}, Parent: &ParentInput{ID: 44}},
			check: func(t *testing.T, got CommentCreate) {
				t.Helper()
				if got.Parent == nil || got.Parent.ID != 44 {
					t.Fatalf("Parent = %#v, want id 44", got.Parent)
				}
			},
		},
		{
			name: "inline",
			input: CommentCreate{
				Content: ContentInput{Raw: "Question"},
				Inline: &InlineInput{
					Path: "internal/cli/pr.go",
					To:   intPtr(12),
				},
			},
			check: func(t *testing.T, got CommentCreate) {
				t.Helper()
				if got.Inline == nil || got.Inline.Path != "internal/cli/pr.go" {
					t.Fatalf("Inline = %#v, want path", got.Inline)
				}
				if got.Inline.To == nil || *got.Inline.To != 12 {
					t.Fatalf("Inline.To = %#v, want 12", got.Inline.To)
				}
				if got.Inline.From != nil {
					t.Fatalf("Inline.From = %#v, want nil", got.Inline.From)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Fatalf("method = %s, want POST", r.Method)
				}
				if r.URL.Path != "/repositories/ws/repo/pullrequests/7/comments" {
					t.Fatalf("path = %s, want comments endpoint", r.URL.Path)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Fatalf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
				}
				var got CommentCreate
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatalf("Decode() error = %v", err)
				}
				tt.check(t, got)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":55,"content":{"raw":"created"}}`)
			}))
			defer server.Close()

			client := newClientForTest(server.URL, server.Client())
			comment, err := client.CreatePullRequestComment("ws", "repo", 7, tt.input)
			if err != nil {
				t.Fatalf("CreatePullRequestComment() error = %v", err)
			}
			if comment.ID != 55 {
				t.Fatalf("comment.ID = %d, want 55", comment.ID)
			}
		})
	}
}

func TestReviewActionEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		call   func(*Client) error
	}{
		{
			name:   "approve",
			method: http.MethodPost,
			path:   "/repositories/ws/repo/pullrequests/7/approve",
			call: func(client *Client) error {
				_, err := client.ApprovePullRequest("ws", "repo", 7)
				return err
			},
		},
		{
			name:   "unapprove",
			method: http.MethodDelete,
			path:   "/repositories/ws/repo/pullrequests/7/approve",
			call:   func(client *Client) error { return client.UnapprovePullRequest("ws", "repo", 7) },
		},
		{
			name:   "request changes",
			method: http.MethodPost,
			path:   "/repositories/ws/repo/pullrequests/7/request-changes",
			call: func(client *Client) error {
				_, err := client.RequestPullRequestChanges("ws", "repo", 7)
				return err
			},
		},
		{
			name:   "clear change request",
			method: http.MethodDelete,
			path:   "/repositories/ws/repo/pullrequests/7/request-changes",
			call:   func(client *Client) error { return client.ClearPullRequestChanges("ws", "repo", 7) },
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tt.method {
					t.Fatalf("method = %s, want %s", r.Method, tt.method)
				}
				if r.URL.Path != tt.path {
					t.Fatalf("path = %s, want %s", r.URL.Path, tt.path)
				}
				if tt.method == http.MethodDelete {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"type":"participant","role":"REVIEWER","approved":true,"state":"approved"}`)
			}))
			defer server.Close()

			client := newClientForTest(server.URL, server.Client())
			if err := tt.call(client); err != nil {
				t.Fatalf("call() error = %v", err)
			}
		})
	}
}

func intPtr(value int) *int {
	return &value
}
