package jira

import (
	"context"
	"fmt"
	"strings"

	jiraapi "github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/ops"
)

// LinkDirection says which side of a link type's wording issue is on.
type LinkDirection string

const (
	LinkOutward LinkDirection = "outward"
	LinkInward  LinkDirection = "inward"
)

func (LinkDirection) Enum() []string {
	return []string{string(LinkOutward), string(LinkInward)}
}

// LinkInput is the input to jira_link.
type LinkInput struct {
	IssueRef
	Type      string        `json:"type" jsonschema:"link type name, any case, numeric prefix optional (Relates finds 2 Relates)"`
	Target    string        `json:"target" jsonschema:"the other issue key"`
	Direction LinkDirection `json:"direction,omitempty" jsonschema:"outward (default): issue <outward verb> target, e.g. blocks; inward: issue <inward verb> target, e.g. is blocked by"`
}

// Linked is the receipt for a link, read back from the issue.
type Linked struct {
	Issue  string `json:"issue"`
	Target string `json:"target"`
	Type   string `json:"type"`
	LinkID string `json:"link_id,omitempty"`
	// Reads is the link as Jira now shows it on issue.
	Reads string `json:"reads"`
}

var Link = &ops.Op[LinkInput, *Linked]{
	Meta: ops.Meta{
		Name: "jira_link",
		Description: "Link two Jira issues by link type name. direction says which way the type's wording reads; " +
			"the receipt shows the link as Jira stored it, e.g. \"CSM-1 blocks CSM-2\".",
		Surfaces: ops.Both,
	},
	Run: link,
}

func link(ctx context.Context, env ops.Env, in LinkInput) (*Linked, error) {
	key, err := in.Key()
	if err != nil {
		return nil, err
	}
	target, err := issueKey("target", in.Target)
	if err != nil {
		return nil, err
	}
	if key == target {
		return nil, ops.Errorf(ops.Invalid, "cannot link %s to itself", key)
	}
	if strings.TrimSpace(in.Type) == "" {
		return nil, ops.Errorf(ops.Invalid, "type is required: give a link type name such as Blocks or Relates")
	}
	client, err := env.Jira(ctx)
	if err != nil {
		return nil, err
	}
	types, err := client.ListIssueLinkTypes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list link types: %w", err)
	}
	linkType, err := jiraapi.ResolveLinkType(types, in.Type)
	if err != nil {
		return nil, invalid(err)
	}
	// Jira's create body reads inwardIssue <outward> outwardIssue: "A blocks
	// B" posts inwardIssue A, outwardIssue B.
	subject, object, verb := key, target, linkType.Outward
	if in.Direction == LinkInward {
		subject, object, verb = target, key, linkType.Inward
	}
	if err := client.CreateIssueLink(ctx, jiraapi.LinkInput{TypeName: linkType.Name, InwardIssue: subject, OutwardIssue: object}); err != nil {
		return nil, err
	}
	receipt := &Linked{Issue: key, Target: target, Type: linkType.Name}
	issue, err := client.GetIssue(ctx, key, jiraapi.GetIssueOptions{Fields: []string{"issuelinks"}})
	if err != nil {
		return nil, fmt.Errorf("jira created the %s link but reading it back from %s failed: %w", linkType.Name, key, err)
	}
	for _, l := range issue.Fields.IssueLinks {
		if l.Type.ID != linkType.ID {
			continue
		}
		switch {
		case in.Direction != LinkInward && l.OutwardIssue != nil && strings.EqualFold(l.OutwardIssue.Key, target):
			receipt.LinkID, receipt.Reads = l.ID, fmt.Sprintf("%s %s %s", key, l.Type.Outward, target)
		case in.Direction == LinkInward && l.InwardIssue != nil && strings.EqualFold(l.InwardIssue.Key, target):
			receipt.LinkID, receipt.Reads = l.ID, fmt.Sprintf("%s %s %s", key, l.Type.Inward, target)
		}
	}
	if receipt.Reads == "" {
		return nil, ops.Errorf(ops.Upstream,
			"jira accepted the link, but %s's links do not show %q; check the issue in Jira", key, fmt.Sprintf("%s %s %s", key, verb, target))
	}
	return receipt, nil
}

// AssignInput is the input to jira_assign.
type AssignInput struct {
	IssueRef
	Assignee string `json:"assignee" jsonschema:"me, email, name, account ID, or none to unassign"`
}

// Assigned is the receipt for an assignment.
type Assigned struct {
	Key       string `json:"key"`
	Assignee  string `json:"assignee"`
	AccountID string `json:"account_id,omitempty"`
}

var Assign = &ops.Op[AssignInput, *Assigned]{
	Meta: ops.Meta{
		Name:        "jira_assign",
		Description: "Assign a Jira issue to me, or to a user found by email or name among those assignable to it; none unassigns.",
		Surfaces:    ops.Both,
	},
	Run: func(ctx context.Context, env ops.Env, in AssignInput) (*Assigned, error) {
		key, err := in.Key()
		if err != nil {
			return nil, err
		}
		client, err := env.Jira(ctx)
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(strings.TrimSpace(in.Assignee), unassigned) {
			if err := client.AssignIssue(ctx, key, ""); err != nil {
				return nil, err
			}
			return &Assigned{Key: key, Assignee: "Unassigned"}, nil
		}
		user, err := resolveUser(ctx, client, in.Assignee, jiraapi.AssignableSearch{IssueKey: key})
		if err != nil {
			return nil, err
		}
		if err := client.AssignIssue(ctx, key, user.AccountID); err != nil {
			return nil, err
		}
		name := user.DisplayName
		if name == "" {
			name = user.AccountID
		}
		return &Assigned{Key: key, Assignee: name, AccountID: user.AccountID}, nil
	},
}
