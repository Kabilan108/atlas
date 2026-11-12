package pr

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
)

var (
	uuidPattern      = regexp.MustCompile(`^\{?[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\}?$`)
	accountIDPattern = regexp.MustCompile(`^([0-9a-f]{24}|[0-9]+:[^\s]+)$`)
)

// isUserID reports whether who is a Bitbucket UUID ({...}) or account ID,
// which /users looks up directly; anything else is a name.
func isUserID(who string) bool {
	return strings.HasPrefix(who, "{") && strings.HasSuffix(who, "}") || uuidPattern.MatchString(who) || accountIDPattern.MatchString(who)
}

// reviewerResolver turns reviewer inputs into users: IDs through /users,
// names through the workspace's members, which it lists at most once.
type reviewerResolver struct {
	client    *bitbucket.Client
	workspace string
	members   []bitbucket.User
	listed    bool
}

// resolveReviewers looks up each reviewer (account ID, UUID, or exact
// display name; a leading @ is ignored) for a pull request in workspace,
// keeping each person once.
func resolveReviewers(ctx context.Context, client *bitbucket.Client, workspace string, inputs []string) ([]bitbucket.User, error) {
	resolver := &reviewerResolver{client: client, workspace: workspace}
	reviewers := make([]bitbucket.User, 0, len(inputs))
	for _, input := range inputs {
		user, err := resolver.resolve(ctx, input)
		if err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(reviewers, user.SharesStableIdentity) {
			reviewers = append(reviewers, user)
		}
	}
	return reviewers, nil
}

func (r *reviewerResolver) resolve(ctx context.Context, input string) (bitbucket.User, error) {
	who := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), "@"))
	if who == "" {
		return bitbucket.User{}, ops.Errorf(ops.Invalid, "reviewer %q is empty", input)
	}
	if isUserID(who) {
		user, err := r.client.GetUser(ctx, who)
		if err != nil {
			return bitbucket.User{}, fmt.Errorf("failed to resolve reviewer %q: %w", input, err)
		}
		return *user, nil
	}
	if !r.listed {
		members, err := r.client.ListWorkspaceMembers(ctx, r.workspace, nil)
		if err != nil {
			return bitbucket.User{}, fmt.Errorf("failed to look up reviewer %q among %s's members (account IDs and UUIDs need no lookup): %w", input, r.workspace, err)
		}
		r.members, r.listed = members, true
	}
	return matchName(r.members, who, "workspace member", r.workspace)
}

// matchName finds the one user whose display name, or failing that
// nickname, is who, ignoring case and spacing.
func matchName(users []bitbucket.User, who, kind, where string) (bitbucket.User, error) {
	want := normalizeName(who)
	for _, name := range []func(bitbucket.User) string{
		func(u bitbucket.User) string { return u.DisplayName },
		func(u bitbucket.User) string { return u.Nickname },
	} {
		var hits []bitbucket.User
		for _, user := range users {
			if normalizeName(name(user)) == want {
				hits = append(hits, user)
			}
		}
		switch len(hits) {
		case 0:
			continue
		case 1:
			return hits[0], nil
		}
		return bitbucket.User{}, ops.Errorf(ops.Invalid, "reviewer %q matches %d %ss: %s; give an account ID", who, len(hits), kind, candidates(hits))
	}
	var near []bitbucket.User
	for _, user := range users {
		if strings.Contains(normalizeName(user.DisplayName), want) || strings.Contains(normalizeName(user.Nickname), want) {
			near = append(near, user)
		}
	}
	message := fmt.Sprintf("no %s of %s is named %q", kind, where, who)
	if len(near) > 0 && len(near) <= 10 {
		message += "; did you mean " + candidates(near)
	}
	return bitbucket.User{}, ops.Errorf(ops.Invalid, "%s", message)
}

func candidates(users []bitbucket.User) string {
	names := make([]string, len(users))
	for i, user := range users {
		id := user.AccountID
		if id == "" {
			id = user.UUID
		}
		names[i] = fmt.Sprintf("%s (%s)", user.DisplayName, id)
	}
	return strings.Join(names, ", ")
}

func normalizeName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}
