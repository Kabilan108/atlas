package cli

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	jiraapi "github.com/kabilan108/atlas/internal/jira"
	"github.com/kabilan108/atlas/internal/ops"
	jiraops "github.com/kabilan108/atlas/internal/ops/jira"
	"github.com/spf13/cobra"
)

func newJiraCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jira",
		Short: "Work with Jira issues",
	}
	cmd.AddCommand(
		newJiraViewCmd(),
		newJiraSearchCmd(),
		newJiraCreateCmd(),
		newJiraEditCmd(),
		newJiraTransitionsCmd(),
		newJiraTransitionCmd(),
		newJiraCommentCmd(),
		newJiraInternalCommentCmd(),
		newJiraLinkCmd(),
		newJiraAssignCmd(),
		newJiraFieldsCmd(),
		newJiraWorklogCmd(),
		newJiraAttachCmd(),
		newJiraWhoamiCmd(),
		newJiraProjectsCmd(),
	)
	return cmd
}

func newJiraViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <issue>",
		Short: "View an issue",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().IntP("comments", "c", 0, "Number of recent comments to show (default 10, -1 for the maximum, 100)")
	cmd.Flags().Bool("history", false, "Include the change history")
	return bind(cmd, binding[jiraops.ViewInput, *jiraops.ViewResult]{
		op: jiraops.View,
		input: func(cmd *cobra.Command, args []string, _ *cliEnv) (jiraops.ViewInput, error) {
			comments, _ := cmd.Flags().GetInt("comments")
			comments = allMeansMax(comments, jiraops.MaxViewComments)
			history, _ := cmd.Flags().GetBool("history")
			return jiraops.ViewInput{IssueRef: jiraops.IssueRef{Issue: args[0]}, Comments: comments, History: history}, nil
		},
		pager: func(result *jiraops.ViewResult) string { return result.Issue.Key + ".md" },
	})
}

func newJiraSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <jql>",
		Short: "Search issues with JQL",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().StringSlice("fields", nil, "Extra columns by field name or ID (comma-separated)")
	cmd.Flags().IntP("limit", "L", 0, "Maximum issues (default 50, -1 for the maximum, 1000)")
	cmd.Flags().String("page-token", "", "Continue from a previous search's page token")
	return bind(cmd, binding[jiraops.SearchInput, *jiraops.SearchResult]{
		op: jiraops.Search,
		input: func(cmd *cobra.Command, args []string, _ *cliEnv) (jiraops.SearchInput, error) {
			fields, _ := cmd.Flags().GetStringSlice("fields")
			limit, _ := cmd.Flags().GetInt("limit")
			limit = allMeansMax(limit, jiraops.MaxSearchResults)
			token, _ := cmd.Flags().GetString("page-token")
			return jiraops.SearchInput{JQL: args[0], Fields: fields, Limit: limit, PageToken: token}, nil
		},
	})
}

func newJiraCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an issue",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringP("project", "p", "", "Project key")
	cmd.Flags().StringP("type", "t", "", "Issue type name")
	cmd.Flags().StringP("summary", "s", "", "Summary")
	addJiraBodyFlags(cmd, "Description in markdown")
	cmd.Flags().String("priority", "", "Priority name")
	cmd.Flags().StringP("assignee", "a", "", "Assignee: me, email, name, or account ID")
	cmd.Flags().String("parent", "", "Parent issue key")
	cmd.Flags().StringArrayP("label", "l", nil, "Label (repeatable)")
	addJiraFieldFlag(cmd)
	for _, name := range []string{"project", "type", "summary"} {
		_ = cmd.MarkFlagRequired(name)
	}
	return bind(cmd, binding[jiraops.CreateInput, *jiraops.Created]{
		op: jiraops.Create,
		input: func(cmd *cobra.Command, _ []string, _ *cliEnv) (jiraops.CreateInput, error) {
			body, err := jiraBody(cmd, false)
			if err != nil {
				return jiraops.CreateInput{}, err
			}
			fields, err := jiraFields(cmd)
			if err != nil {
				return jiraops.CreateInput{}, err
			}
			in := jiraops.CreateInput{Body: body, Fields: fields}
			in.Project, _ = cmd.Flags().GetString("project")
			in.Type, _ = cmd.Flags().GetString("type")
			in.Summary, _ = cmd.Flags().GetString("summary")
			in.Priority, _ = cmd.Flags().GetString("priority")
			in.Assignee, _ = cmd.Flags().GetString("assignee")
			in.Parent, _ = cmd.Flags().GetString("parent")
			in.Labels, _ = cmd.Flags().GetStringArray("label")
			return in, nil
		},
		human: func(w io.Writer, out *jiraops.Created) error {
			_, err := fmt.Fprintf(w, "Created %s: %s\n", out.Key, out.URL)
			return err
		},
	})
}

func newJiraEditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "edit <issue>",
		Short: "Edit an issue",
		Args:  cobra.ExactArgs(1),
	}
	cmd.Flags().StringP("summary", "s", "", "New summary")
	addJiraBodyFlags(cmd, "New description in markdown")
	cmd.Flags().Bool("append", false, "Add the body after the current description instead of replacing it")
	cmd.Flags().Bool("force", false, "Replace a description even if it has content markdown cannot hold")
	cmd.Flags().StringArrayP("label", "l", nil, "Label to add, or -label to remove (repeatable)")
	cmd.Flags().StringP("assignee", "a", "", "Assignee: me, email, name, account ID, or none")
	addJiraFieldFlag(cmd)
	cmd.MarkFlagsMutuallyExclusive("append", "force")
	return bind(cmd, binding[jiraops.EditInput, *jiraops.Edited]{
		op: jiraops.Edit,
		input: func(cmd *cobra.Command, args []string, _ *cliEnv) (jiraops.EditInput, error) {
			body, err := jiraBody(cmd, false)
			if err != nil {
				return jiraops.EditInput{}, err
			}
			fields, err := jiraFields(cmd)
			if err != nil {
				return jiraops.EditInput{}, err
			}
			in := jiraops.EditInput{IssueRef: jiraops.IssueRef{Issue: args[0]}, Body: body, Fields: fields}
			in.Summary, _ = cmd.Flags().GetString("summary")
			in.Labels, _ = cmd.Flags().GetStringArray("label")
			in.Assignee, _ = cmd.Flags().GetString("assignee")
			in.Force, _ = cmd.Flags().GetBool("force")
			if appendBody, _ := cmd.Flags().GetBool("append"); appendBody {
				in.BodyMode = jiraops.BodyAppend
			}
			return in, nil
		},
		human: func(w io.Writer, out *jiraops.Edited) error {
			_, err := fmt.Fprintf(w, "Updated %s (%s): %s\n", out.Key, strings.Join(out.Changed, ", "), out.URL)
			return err
		},
	})
}

func newJiraTransitionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transitions <issue>",
		Short: "List the transitions an issue allows",
		Args:  cobra.ExactArgs(1),
	}
	return bind(cmd, binding[jiraops.IssueRef, *jiraops.TransitionsResult]{
		op:    jiraops.Transitions,
		input: issueArg,
	})
}

func newJiraTransitionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "transition <issue> <transition-or-status>",
		Aliases: []string{"move"},
		Short:   "Move an issue by transition name or target status",
		Args:    cobra.ExactArgs(2),
	}
	addJiraFieldFlag(cmd)
	return bind(cmd, binding[jiraops.TransitionInput, *jiraops.Transitioned]{
		op: jiraops.Transition,
		input: func(cmd *cobra.Command, args []string, _ *cliEnv) (jiraops.TransitionInput, error) {
			fields, err := jiraFields(cmd)
			if err != nil {
				return jiraops.TransitionInput{}, err
			}
			return jiraops.TransitionInput{IssueRef: jiraops.IssueRef{Issue: args[0]}, Transition: args[1], Fields: fields}, nil
		},
		human: func(w io.Writer, out *jiraops.Transitioned) error {
			_, err := fmt.Fprintf(w, "Moved %s with %q; status is now %s\n", out.Key, out.Transition, out.Status)
			return err
		},
	})
}

func newJiraCommentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comment <issue>",
		Short: "Add a public comment",
		Long:  "Add a public comment. On service desk issues customers can read it; use 'atlas jira internal-comment' for agent-only notes.",
		Args:  cobra.ExactArgs(1),
	}
	return bindJiraComment(cmd, jiraops.Comment)
}

func newJiraInternalCommentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "internal-comment <issue>",
		Short: "Add an internal note to a service desk issue",
		Long:  "Add an internal note that only agents see. Refuses issues outside service desk projects and fails if Jira does not confirm the note is internal.",
		Args:  cobra.ExactArgs(1),
	}
	return bindJiraComment(cmd, jiraops.InternalComment)
}

func bindJiraComment(cmd *cobra.Command, op *ops.Op[jiraops.CommentInput, *jiraops.Commented]) *cobra.Command {
	addJiraBodyFlags(cmd, "Comment in markdown")
	return bind(cmd, binding[jiraops.CommentInput, *jiraops.Commented]{
		op: op,
		input: func(cmd *cobra.Command, args []string, _ *cliEnv) (jiraops.CommentInput, error) {
			body, err := jiraBody(cmd, true)
			if err != nil {
				return jiraops.CommentInput{}, err
			}
			return jiraops.CommentInput{IssueRef: jiraops.IssueRef{Issue: args[0]}, Body: body}, nil
		},
		human: func(w io.Writer, out *jiraops.Commented) error {
			kind := "comment"
			if out.Internal {
				kind = "internal note"
			}
			_, err := fmt.Fprintf(w, "Added %s %s to %s: %s\n", kind, out.ID, out.Key, out.URL)
			return err
		},
	})
}

func newJiraLinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link <issue> <target> <type>",
		Short: "Link two issues",
		Long:  "Link two issues by link type name. By default the issue takes the type's outward wording (issue blocks target); --inward reverses it (issue is blocked by target).",
		Args:  cobra.ExactArgs(3),
	}
	cmd.Flags().Bool("inward", false, "Use the type's inward wording: <issue> is blocked by <target>")
	return bind(cmd, binding[jiraops.LinkInput, *jiraops.Linked]{
		op: jiraops.Link,
		input: func(cmd *cobra.Command, args []string, _ *cliEnv) (jiraops.LinkInput, error) {
			in := jiraops.LinkInput{IssueRef: jiraops.IssueRef{Issue: args[0]}, Target: args[1], Type: args[2]}
			if inward, _ := cmd.Flags().GetBool("inward"); inward {
				in.Direction = jiraops.LinkInward
			}
			return in, nil
		},
		human: func(w io.Writer, out *jiraops.Linked) error {
			_, err := fmt.Fprintf(w, "Linked: %s\n", out.Reads)
			return err
		},
	})
}

func newJiraAssignCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assign <issue> <user>",
		Short: "Assign an issue (me, email, name, account ID, or none)",
		Args:  cobra.ExactArgs(2),
	}
	return bind(cmd, binding[jiraops.AssignInput, *jiraops.Assigned]{
		op: jiraops.Assign,
		input: func(_ *cobra.Command, args []string, _ *cliEnv) (jiraops.AssignInput, error) {
			return jiraops.AssignInput{IssueRef: jiraops.IssueRef{Issue: args[0]}, Assignee: args[1]}, nil
		},
		human: func(w io.Writer, out *jiraops.Assigned) error {
			_, err := fmt.Fprintf(w, "Assigned %s to %s\n", out.Key, out.Assignee)
			return err
		},
	})
}

func newJiraFieldsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fields [issue]",
		Short: "List create fields (-p and -t) or an issue's edit fields",
		Args:  cobra.MaximumNArgs(1),
	}
	cmd.Flags().StringP("project", "p", "", "Project key, for create fields")
	cmd.Flags().StringP("type", "t", "", "Issue type name, with --project")
	return bind(cmd, binding[jiraops.FieldsInput, *jiraops.FieldsResult]{
		op: jiraops.Fields,
		input: func(cmd *cobra.Command, args []string, _ *cliEnv) (jiraops.FieldsInput, error) {
			in := jiraops.FieldsInput{Issue: optionalArg(args)}
			in.Project, _ = cmd.Flags().GetString("project")
			in.Type, _ = cmd.Flags().GetString("type")
			return in, nil
		},
	})
}

func newJiraWorklogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worklog",
		Short: "List and add worklogs",
	}
	list := &cobra.Command{
		Use:   "list <issue>",
		Short: "List every worklog on an issue",
		Args:  cobra.ExactArgs(1),
	}
	add := &cobra.Command{
		Use:   "add [<issue> <duration>]",
		Short: "Log work, or a batch of entries from --entries-file",
		Long: "Log work on an issue (duration like 1h 30m or PT1H30M), or a JSON array of entries\n" +
			`({"issue","started","duration","comment"}) from --entries-file. Entries you already logged are skipped.`,
		Args: func(cmd *cobra.Command, args []string) error {
			if file, _ := cmd.Flags().GetString("entries-file"); file != "" {
				return cobra.NoArgs(cmd, args)
			}
			return cobra.ExactArgs(2)(cmd, args)
		},
	}
	add.Flags().String("started", "", "Start time, ISO 8601 with offset (default now)")
	add.Flags().StringP("comment", "m", "", "Worklog comment in markdown")
	add.Flags().StringP("entries-file", "f", "", "Read a JSON array of entries from a file (- for stdin)")
	add.MarkFlagsMutuallyExclusive("entries-file", "started")
	add.MarkFlagsMutuallyExclusive("entries-file", "comment")
	cmd.AddCommand(
		bind(list, binding[jiraops.IssueRef, *jiraops.WorklogsResult]{op: jiraops.WorklogList, input: issueArg}),
		bind(add, binding[jiraops.WorklogAddInput, *jiraops.WorklogAddResult]{
			op:    jiraops.WorklogAdd,
			input: worklogAddInput,
			human: func(w io.Writer, out *jiraops.WorklogAddResult) error {
				for _, r := range out.Results {
					line := fmt.Sprintf("%d %s %s %s %s", r.Index, r.Status, r.Issue, r.Started, r.Duration)
					if r.WorklogID != "" {
						line += " worklog " + r.WorklogID
					}
					if r.Error != "" {
						line += ": " + r.Error
					}
					if _, err := fmt.Fprintln(w, line); err != nil {
						return err
					}
				}
				return nil
			},
		}),
	)
	return cmd
}

func worklogAddInput(cmd *cobra.Command, args []string, _ *cliEnv) (jiraops.WorklogAddInput, error) {
	if file, _ := cmd.Flags().GetString("entries-file"); file != "" {
		data, err := readBodyFile(file)
		if err != nil {
			return jiraops.WorklogAddInput{}, fmt.Errorf("read entries: %w", err)
		}
		var entries []jiraops.WorklogEntry
		dec := json.NewDecoder(strings.NewReader(string(data)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&entries); err != nil {
			return jiraops.WorklogAddInput{}, fmt.Errorf("read entries from %s: expected a JSON array of {issue, started, duration, comment}: %w", file, err)
		}
		return jiraops.WorklogAddInput{Entries: entries}, nil
	}
	started, _ := cmd.Flags().GetString("started")
	if started == "" {
		started = time.Now().Format(time.RFC3339)
	}
	comment, _ := cmd.Flags().GetString("comment")
	return jiraops.WorklogAddInput{Entries: []jiraops.WorklogEntry{{Issue: args[0], Duration: args[1], Started: started, Comment: comment}}}, nil
}

func newJiraAttachCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attach <issue> <file>...",
		Short: "Attach files to an issue",
		Args:  cobra.MinimumNArgs(2),
	}
	return bind(cmd, binding[jiraops.AttachInput, *jiraops.Attached]{
		op: jiraops.Attach,
		input: func(cmd *cobra.Command, args []string, _ *cliEnv) (jiraops.AttachInput, error) {
			return jiraAttachInput(cmd, args)
		},
		human: func(w io.Writer, out *jiraops.Attached) error {
			for _, a := range out.Attachments {
				if _, err := fmt.Fprintf(w, "Attached %s (%d bytes) to %s: %s\n", a.Filename, a.Size, out.Key, a.URL); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

func newJiraWhoamiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show the Jira account your credentials belong to",
		Args:  cobra.NoArgs,
	}
	return bind(cmd, binding[jiraops.WhoamiInput, *jiraapi.User]{
		op: jiraops.Whoami,
		input: func(*cobra.Command, []string, *cliEnv) (jiraops.WhoamiInput, error) {
			return jiraops.WhoamiInput{}, nil
		},
	})
}

func newJiraProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects",
		Short: "List Jira projects",
		Args:  cobra.NoArgs,
	}
	cmd.Flags().StringP("query", "q", "", "Filter by project name or key")
	cmd.Flags().IntP("limit", "L", 0, "Maximum projects (default 100, -1 for the maximum, 1000)")
	return bind(cmd, binding[jiraops.ProjectsInput, []jiraapi.Project]{
		op: jiraops.Projects,
		input: func(cmd *cobra.Command, _ []string, _ *cliEnv) (jiraops.ProjectsInput, error) {
			query, _ := cmd.Flags().GetString("query")
			limit, _ := cmd.Flags().GetInt("limit")
			return jiraops.ProjectsInput{Query: query, Limit: allMeansMax(limit, jiraops.MaxProjects)}, nil
		},
	})
}

// jiraAttachInput reads the named files, base64-encoded as jira_attach
// takes them, stopping at the per-call limit.
func jiraAttachInput(_ *cobra.Command, args []string) (jiraops.AttachInput, error) {
	in := jiraops.AttachInput{IssueRef: jiraops.IssueRef{Issue: args[0]}}
	if len(args)-1 > jiraops.MaxAttachFiles {
		return in, fmt.Errorf("attach at most %d files per call", jiraops.MaxAttachFiles)
	}
	remaining := int64(jiraops.MaxAttachBytes)
	for _, path := range args[1:] {
		data, err := readAtMost(path, remaining)
		if err != nil {
			return in, err
		}
		remaining -= int64(len(data))
		in.Files = append(in.Files, jiraops.AttachFile{Filename: filepath.Base(path), Content: base64.StdEncoding.EncodeToString(data)})
	}
	return in, nil
}

// readAtMost reads path, failing once it passes limit bytes.
func readAtMost(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s: files total more than %d bytes, the limit per call", path, jiraops.MaxAttachBytes)
	}
	return data, nil
}

// allMeansMax maps the CLI's -1 ("all") to an op's hard maximum; ops have
// no unlimited mode.
func allMeansMax(n, maximum int) int {
	if n == -1 {
		return maximum
	}
	return n
}

func issueArg(_ *cobra.Command, args []string, _ *cliEnv) (jiraops.IssueRef, error) {
	return jiraops.IssueRef{Issue: args[0]}, nil
}

func addJiraBodyFlags(cmd *cobra.Command, usage string) {
	cmd.Flags().StringP("body", "b", "", usage)
	cmd.Flags().StringP("body-file", "F", "", "Read the body from a file (use - to read from stdin)")
	cmd.MarkFlagsMutuallyExclusive("body", "body-file")
}

// jiraBody reads --body or --body-file; required says one must be given.
func jiraBody(cmd *cobra.Command, required bool) (string, error) {
	body, _ := cmd.Flags().GetString("body")
	if file, _ := cmd.Flags().GetString("body-file"); file != "" {
		data, err := readBodyFile(file)
		if err != nil {
			return "", fmt.Errorf("read body: %w", err)
		}
		body = string(data)
	}
	if required && strings.TrimSpace(body) == "" {
		return "", fmt.Errorf("a body is required: use --body or --body-file")
	}
	return body, nil
}

func addJiraFieldFlag(cmd *cobra.Command) {
	cmd.Flags().StringArray("field", nil, `Field by display name as Name=value (repeat a name for a list; a value starting with [ or { is JSON; null clears)`)
}

// jiraFields reads --field Name=value flags. Repeating a name makes a list,
// so --field Product=A --field Product=B sets both options.
func jiraFields(cmd *cobra.Command) (map[string]any, error) {
	values, _ := cmd.Flags().GetStringArray("field")
	if len(values) == 0 {
		return nil, nil
	}
	fields := map[string]any{}
	for _, raw := range values {
		name, text, ok := strings.Cut(raw, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return nil, fmt.Errorf("--field %q: expected Name=value", raw)
		}
		var value any = text
		switch trimmed := strings.TrimSpace(text); {
		case trimmed == "null":
			value = nil
		case strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, "{"):
			if err := json.Unmarshal([]byte(trimmed), &value); err != nil {
				return nil, fmt.Errorf("--field %s: invalid JSON: %w", name, err)
			}
		}
		prev, seen := fields[name]
		if !seen {
			fields[name] = value
			continue
		}
		list, isList := prev.([]any)
		if !isList {
			list = []any{prev}
		}
		if more, ok := value.([]any); ok {
			fields[name] = append(list, more...)
		} else {
			fields[name] = append(list, value)
		}
	}
	return fields, nil
}
