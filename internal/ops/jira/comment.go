package jira

import (
	"context"
	"fmt"

	jiraapi "github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/ops"
)

// serviceDesk is the projectTypeKey of Jira Service Management projects,
// the only ones whose comments can be internal notes.
const serviceDesk = "service_desk"

// CommentInput is the input to jira_comment and jira_internal_comment.
type CommentInput struct {
	IssueRef
	Body string `json:"body" jsonschema:"comment, markdown"`
}

// Commented is the receipt for a new comment.
type Commented struct {
	Key      string `json:"key"`
	ID       string `json:"id"`
	URL      string `json:"url"`
	Internal bool   `json:"internal"`
}

var Comment = &ops.Op[CommentInput, *Commented]{
	Meta: ops.Meta{
		Name: "jira_comment",
		Description: "Add a public comment to a Jira issue (markdown). On service desk issues customers can read it; " +
			"use jira_internal_comment for agent-only notes.",
		Surfaces: ops.Both,
	},
	Run: func(ctx context.Context, env ops.Env, in CommentInput) (*Commented, error) {
		key, body, client, err := commentTarget(ctx, env, in)
		if err != nil {
			return nil, err
		}
		site, err := siteURL(ctx, client)
		if err != nil {
			return nil, err
		}
		comment, err := client.CreateComment(ctx, key, jiraapi.CommentInput{Body: body})
		if err != nil {
			return nil, err
		}
		return &Commented{Key: key, ID: comment.ID, URL: jiraapi.CommentURL(site, key, comment.ID)}, nil
	},
}

var InternalComment = &ops.Op[CommentInput, *Commented]{
	Meta: ops.Meta{
		Name: "jira_internal_comment",
		Description: "Add an internal note (agents only, never customers) to a Jira Service Management issue (markdown). " +
			"Refuses other projects and fails if Jira does not confirm the note is internal.",
		Surfaces: ops.Both,
	},
	Run: internalComment,
}

func internalComment(ctx context.Context, env ops.Env, in CommentInput) (*Commented, error) {
	key, body, client, err := commentTarget(ctx, env, in)
	if err != nil {
		return nil, err
	}
	issue, err := client.GetIssue(ctx, key, jiraapi.GetIssueOptions{Fields: []string{"project"}})
	if err != nil {
		return nil, err
	}
	project := issue.Fields.Project
	if project == nil {
		return nil, ops.Errorf(ops.Upstream, "jira: %s came back without its project; not posting an internal note", key)
	}
	projectType := project.ProjectTypeKey
	if projectType == "" {
		full, err := client.GetProject(ctx, project.Key)
		if err != nil {
			return nil, fmt.Errorf("check %s's project type: %w", key, err)
		}
		projectType = full.ProjectTypeKey
	}
	if projectType != serviceDesk {
		return nil, ops.Errorf(ops.Invalid,
			"%s is in %s, a %s project, not a service desk: internal notes exist only on service desk issues. Use jira_comment for a comment everyone with access can see",
			key, project.Key, projectType)
	}
	site, err := siteURL(ctx, client)
	if err != nil {
		return nil, err
	}
	comment, err := client.CreateComment(ctx, key, jiraapi.CommentInput{
		Body:       body,
		Properties: []jiraapi.EntityProperty{jiraapi.InternalNoteProperty()},
	})
	if err != nil {
		return nil, err
	}
	url := jiraapi.CommentURL(site, key, comment.ID)
	if comment.JSDPublic == nil || *comment.JSDPublic {
		return nil, notConfirmedInternal(key, comment, url)
	}
	return &Commented{Key: key, ID: comment.ID, URL: url, Internal: true}, nil
}

// notConfirmedInternal is the error for a note Jira created without saying
// it is internal. The comment is left in place; whether atlas should delete
// it here is still undecided.
func notConfirmedInternal(key string, comment *jiraapi.Comment, url string) error {
	reported := "did not report jsdPublic"
	if comment.JSDPublic != nil {
		reported = "reported jsdPublic=true"
	}
	return ops.Errorf(ops.Upstream,
		"jira created comment %s on %s but %s, so it MAY BE VISIBLE TO CUSTOMERS: %s. Check it in Jira and delete or restrict it if it is public",
		comment.ID, key, reported, url)
}

func commentTarget(ctx context.Context, env ops.Env, in CommentInput) (string, []byte, *jiraapi.Client, error) {
	key, err := in.Key()
	if err != nil {
		return "", nil, nil, err
	}
	body, err := markdownBody("body", in.Body)
	if err != nil {
		return "", nil, nil, err
	}
	client, err := env.Jira(ctx)
	if err != nil {
		return "", nil, nil, err
	}
	return key, body, client, nil
}
