package jira

import (
	"context"
	"fmt"
	"strings"

	jiraapi "github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/ops"
)

// Ops returns the Jira ops in a stable order.
func Ops() []ops.Def {
	return []ops.Def{
		View,
		Search,
		Create,
		Edit,
		Transitions,
		Transition,
		Comment,
		InternalComment,
		Link,
		Assign,
		Fields,
		WorklogList,
		WorklogAdd,
		Attach,
		Whoami,
		Projects,
	}
}

// WhoamiInput takes no arguments.
type WhoamiInput struct{}

var Whoami = &ops.Op[WhoamiInput, *jiraapi.User]{
	Meta: ops.Meta{
		Name:        "jira_whoami",
		Description: "Show the Jira account the credentials belong to: name, email, account ID and time zone.",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run: func(ctx context.Context, env ops.Env, _ WhoamiInput) (*jiraapi.User, error) {
		client, err := env.Jira(ctx)
		if err != nil {
			return nil, err
		}
		return client.Myself(ctx)
	},
	Markdown: func(u *jiraapi.User) (string, error) {
		var b strings.Builder
		fmt.Fprintf(&b, "**%s**", userName(u))
		if u.EmailAddress != "" {
			fmt.Fprintf(&b, " (%s)", u.EmailAddress)
		}
		fmt.Fprintf(&b, "\n\n- Account ID: %s\n", u.AccountID)
		if u.TimeZone != "" {
			fmt.Fprintf(&b, "- Time zone: %s\n", u.TimeZone)
		}
		if !u.Active {
			b.WriteString("- Inactive\n")
		}
		return b.String(), nil
	},
}

const defaultProjectLimit = 100

// ProjectsInput is the input to jira_projects.
type ProjectsInput struct {
	Query string `json:"query,omitempty" jsonschema:"filter by project name or key"`
	Limit int    `json:"limit,omitempty" jsonschema:"max projects (default 100, max 1000)"`
}

var Projects = &ops.Op[ProjectsInput, []jiraapi.Project]{
	Meta: ops.Meta{
		Name:        "jira_projects",
		Description: "List the Jira projects you can see: key, name and type (service_desk projects take internal comments).",
		Surfaces:    ops.Both,
		ReadOnly:    true,
	},
	Run: func(ctx context.Context, env ops.Env, in ProjectsInput) ([]jiraapi.Project, error) {
		client, err := env.Jira(ctx)
		if err != nil {
			return nil, err
		}
		limit, err := boundedCount("limit", in.Limit, defaultProjectLimit, MaxProjects)
		if err != nil {
			return nil, err
		}
		return client.ListProjects(ctx, jiraapi.ProjectListOptions{Query: strings.TrimSpace(in.Query), OrderBy: "key", Limit: limit})
	},
	Markdown: func(projects []jiraapi.Project) (string, error) {
		if len(projects) == 0 {
			return "No projects match.\n", nil
		}
		var b strings.Builder
		b.WriteString("| Key | Name | Type |\n| --- | --- | --- |\n")
		for _, p := range projects {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", p.Key, cell(p.Name), cell(p.ProjectTypeKey))
		}
		return b.String(), nil
	},
}
