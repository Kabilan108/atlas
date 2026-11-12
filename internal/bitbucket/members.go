package bitbucket

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

type MemberListOptions struct {
	// Name keeps members whose display name, nickname, or username contains it,
	// ignoring case. Bitbucket's members endpoint only filters by email (and only
	// for admins), so this match runs client-side.
	Name string
	// Limit caps the members returned after filtering. Zero returns all.
	Limit int
}

type workspaceMembership struct {
	User User `json:"user"`
}

func (c *Client) ListWorkspaceMembers(ctx context.Context, workspace string, opts *MemberListOptions) ([]User, error) {
	path := fmt.Sprintf("/workspaces/%s/members?pagelen=%d", url.PathEscape(workspace), maxPageLen)
	needle := ""
	limit := 0
	if opts != nil {
		needle = strings.ToLower(strings.TrimSpace(opts.Name))
		limit = opts.Limit
	}

	pageLimit := 0
	if needle == "" {
		pageLimit = limit
	}
	memberships, err := collectPages[workspaceMembership](ctx, c, path, pageLimit, "workspace members")
	if err != nil {
		return nil, err
	}

	users := make([]User, 0, len(memberships))
	for _, membership := range memberships {
		if needle != "" && !userNameContains(membership.User, needle) {
			continue
		}
		users = append(users, membership.User)
		if limit > 0 && len(users) >= limit {
			break
		}
	}
	return users, nil
}

func userNameContains(user User, lowerNeedle string) bool {
	for _, name := range []string{user.DisplayName, user.Nickname, user.Username} {
		if strings.Contains(strings.ToLower(name), lowerNeedle) {
			return true
		}
	}
	return false
}
