package repo

import (
	"context"
	"fmt"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/output"
)

const defaultUserLimit = 20

// UserSearchInput is the input to user_search.
type UserSearchInput struct {
	Workspace string `json:"workspace,omitempty" jsonschema:"default: the default workspace"`
	Query     string `json:"query" jsonschema:"case-insensitive substring of display name, nickname, or username"`
	Limit     int    `json:"limit,omitempty" jsonschema:"default 20, max 100"`
}

// UserSearchResult is the matching workspace members. More means more
// members match; ScanCapped that the search stopped after reading
// maxMembersScanned members, so matches past them were never seen.
type UserSearchResult struct {
	Workspace  string           `json:"workspace"`
	Query      string           `json:"query"`
	Users      []bitbucket.User `json:"users"`
	More       bool             `json:"more,omitempty"`
	ScanCapped bool             `json:"scan_capped,omitempty"`

	limit int
}

var UserSearch = &ops.Op[UserSearchInput, *UserSearchResult]{
	Meta: ops.Meta{
		Name:        "user_search",
		Description: "Find Bitbucket workspace members by name; returns account IDs and UUIDs to use as reviewers or mentions.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run:      searchUsers,
	Markdown: (*UserSearchResult).Markdown,
}

func searchUsers(ctx context.Context, env ops.Env, in UserSearchInput) (*UserSearchResult, error) {
	workspace, err := ops.Workspace(in.Workspace, env)
	if err != nil {
		return nil, err
	}
	query := strings.TrimSpace(in.Query)
	if query == "" {
		return nil, ops.Errorf(ops.Invalid, "query is required")
	}
	limit, err := limitOrDefault(in.Limit, defaultUserLimit, maxUserLimit)
	if err != nil {
		return nil, err
	}
	client, err := env.Bitbucket(ctx)
	if err != nil {
		return nil, err
	}
	users, scan, err := client.ListWorkspaceMembers(ctx, workspace, &bitbucket.MemberListOptions{Name: query, Limit: limit + 1, MaxScan: maxMembersScanned})
	if err != nil {
		return nil, err
	}
	result := &UserSearchResult{Workspace: workspace, Query: query, ScanCapped: scan.Capped, limit: limit}
	result.Users, result.More = trimMore(users, limit)
	return result, nil
}

// Markdown renders the members as a table.
func (r *UserSearchResult) Markdown() (string, error) {
	var text string
	if len(r.Users) == 0 {
		text = fmt.Sprintf("No members of %s match %q.\n", r.Workspace, r.Query)
	} else {
		rows := make([][]string, 0, len(r.Users))
		for _, user := range r.Users {
			rows = append(rows, []string{user.DisplayName, user.Nickname, user.AccountID, user.UUID})
		}
		text = output.MarkdownTable([]string{"Name", "Nickname", "Account ID", "UUID"}, rows) + moreNote(r.More, r.limit)
	}
	if r.ScanCapped {
		text += fmt.Sprintf("\nSearched only the first %d members of %s; use a more specific query.\n", maxMembersScanned, r.Workspace)
	}
	return text, nil
}

// WhoamiInput is the input to whoami, which takes none.
type WhoamiInput struct{}

var Whoami = &ops.Op[WhoamiInput, *bitbucket.User]{
	Meta: ops.Meta{
		Name:        "whoami",
		Description: "Show the Bitbucket account the credentials belong to: display name, username, account ID, UUID.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run: func(ctx context.Context, env ops.Env, _ WhoamiInput) (*bitbucket.User, error) {
		client, err := env.Bitbucket(ctx)
		if err != nil {
			return nil, err
		}
		return client.GetCurrentUser(ctx)
	},
	Markdown: func(user *bitbucket.User) (string, error) {
		var b strings.Builder
		fmt.Fprintf(&b, "Display name: %s\n", user.DisplayName)
		if user.Username != "" {
			fmt.Fprintf(&b, "Username: %s\n", user.Username)
		}
		if user.Nickname != "" {
			fmt.Fprintf(&b, "Nickname: %s\n", user.Nickname)
		}
		fmt.Fprintf(&b, "Account ID: %s\nUUID: %s\n", user.AccountID, user.UUID)
		return b.String(), nil
	},
}
