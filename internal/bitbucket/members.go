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
	// Limit caps the members returned after filtering. Zero means
	// MaxListItems.
	Limit int
	// MaxScan caps the members read while filtering by Name. Zero means
	// MaxListItems.
	MaxScan int
}

type workspaceMembership struct {
	User User `json:"user"`
}

// ListWorkspaceMembers stops paging once it has Limit matches or has read
// MaxScan members, and says which in the Scan.
func (c *Client) ListWorkspaceMembers(ctx context.Context, workspace string, opts *MemberListOptions) ([]User, Scan, error) {
	path := fmt.Sprintf("/workspaces/%s/members?pagelen=%d", url.PathEscape(workspace), maxPageLen)
	needle := ""
	limit, maxScan := 0, 0
	if opts != nil {
		needle = strings.ToLower(strings.TrimSpace(opts.Name))
		limit, maxScan = opts.Limit, opts.MaxScan
	}
	var keep func(workspaceMembership) bool
	if needle != "" {
		keep = func(membership workspaceMembership) bool { return userNameContains(membership.User, needle) }
	}

	memberships, scan, err := scanPages(ctx, c.pages(), path, keep, limit, maxScan, "workspace members")
	if err != nil {
		return nil, scan, err
	}
	users := make([]User, 0, len(memberships))
	for _, membership := range memberships {
		users = append(users, membership.User)
	}
	return users, scan, nil
}

func userNameContains(user User, lowerNeedle string) bool {
	for _, name := range []string{user.DisplayName, user.Nickname, user.Username} {
		if strings.Contains(strings.ToLower(name), lowerNeedle) {
			return true
		}
	}
	return false
}
