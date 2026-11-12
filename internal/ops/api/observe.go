package api

import (
	"context"
	"regexp"
	"strings"
)

// Call is one api op call, with its path reduced to a template.
type Call struct {
	Method   string
	Template string
}

type observerKey struct{}

// WithObserver returns a context whose api calls are reported to observe,
// which a server can use to count the paths agents fetch most. observe may
// run on any goroutine.
func WithObserver(ctx context.Context, observe func(Call)) context.Context {
	return context.WithValue(ctx, observerKey{}, observe)
}

func observe(ctx context.Context, call Call) {
	if fn, ok := ctx.Value(observerKey{}).(func(Call)); ok && fn != nil {
		fn(call)
	}
}

var (
	numberPattern = regexp.MustCompile(`^\d+$`)
	uuidPattern   = regexp.MustCompile(`^(\{|%7[bB])?[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}(\}|%7[dD])?$`)
	hashPattern   = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	issueKey      = regexp.MustCompile(`^[A-Z][A-Z0-9]+-\d+$`)
)

// namedSegments are path segments followed by names rather than API words:
// after "repositories" come {workspace} and {repo}.
var namedSegments = map[string][]string{
	"repositories": {"{workspace}", "{repo}"},
	"workspaces":   {"{workspace}"},
	"snippets":     {"{workspace}", "{id}"},
	"users":        {"{user}"},
	"branches":     {"{name}"},
	"tags":         {"{name}"},
	"src":          {"{commit}"},
	"diff":         {"{spec}"},
	"diffstat":     {"{spec}"},
	"patch":        {"{spec}"},
	"commit":       {"{commit}"},
	"issue":        {"{issue}"},
	"project":      {"{project}"},
}

// apiVersions are the versions kept after "api" or "agile": /rest/api/3/...
var apiVersions = map[string]bool{"2": true, "3": true, "1.0": true, "latest": true}

// apiWords are the structural words of the Bitbucket 2.0 and Jira REST
// paths. A template keeps only these; every other segment is a name, a
// key, an email, or an ID that a caller chose, and becomes a placeholder,
// so a log line never records what the path named.
var apiWords = setOf(
	// Bitbucket
	"2.0", "repositories", "workspaces", "snippets", "users", "user", "teams",
	"pullrequests", "activity", "approve", "comments", "resolve", "commits",
	"decline", "merge", "task-status", "request-changes", "statuses", "build",
	"tasks", "refs", "branches", "tags", "src", "diff", "diffstat", "patch",
	"commit", "filehistory", "merge-base", "pipelines", "pipelines_config",
	"steps", "log", "logs", "test_reports", "test_cases", "test_case_reasons",
	"stopPipeline", "schedules", "executions", "caches", "content-uri",
	"variables", "deployments", "deployments_config", "environments",
	"downloads", "forks", "hooks", "hook_events", "issues", "milestones",
	"versions", "components", "default-reviewers",
	"effective-default-reviewers", "branch-restrictions", "branching-model",
	"effective-branching-model", "settings", "override-settings",
	"deploy-keys", "permissions-config", "groups", "members", "projects",
	"permissions", "reports", "annotations", "search", "code", "watchers",
	"watching", "emails", "ssh-keys", "gpg-keys", "runners", "ssh",
	"key_pair", "known_hosts", "properties", "current-user", "repository",
	"workspace", "pullrequest",
	// Jira
	"rest", "api", "agile", "servicedeskapi", "issue", "jql",
	"approximate-count", "field", "fields", "project", "myself", "property",
	"comment", "worklog", "changelog", "list", "transitions", "attachment",
	"attachments", "content", "thumbnail", "meta", "expand", "raw",
	"issuetype", "issuetypes", "priority", "priorities", "status",
	"statuscategory", "resolution", "resolutions", "filter", "filters",
	"favourite", "my", "dashboard", "board", "sprint", "epic", "backlog",
	"configuration", "request", "requesttype", "servicedesk", "customer",
	"organization", "participant", "sla", "approval", "queue",
	"knowledgebase", "article", "info", "issueLink", "issueLinkType",
	"remotelink", "votes", "createmeta", "editmeta", "notify", "assignee",
	"assignable", "multiProjectSearch", "picker", "mypermissions", "group",
	"groupuserpicker", "member", "serverInfo", "application-properties",
	"label", "version", "component", "role", "roles", "notificationscheme",
	"permissionscheme", "workflow", "workflows", "workflowscheme", "screens",
	"screen", "tabs", "avatar", "avatars", "bulk", "bulkfetch", "expression",
	"eval", "analyse", "rank", "estimation", "feature", "features", "deleted",
	"updated", "securitylevel", "hierarchy", "type", "types", "context",
	"option", "options", "default", "check", "locale", "timetracking",
	"columns", "auditing", "record", "autocompletedata", "suggestions",
	"parse", "match", "preference", "mypreferences", "task", "cancel",
	"universal_avatar", "view", "notification", "archive", "restore",
	"subtasks", "requestparticipant", "issuesecurityschemes",
)

func setOf(words ...string) map[string]bool {
	set := make(map[string]bool, len(words))
	for _, word := range words {
		set[word] = true
	}
	return set
}

// Template drops the query and keeps only the API's own words, replacing
// every other segment with a placeholder, so calls that differ only in
// which object they name count as one and no name, key, email, or ID is
// recorded: /2.0/repositories/acme/app/pullrequests/42 becomes
// /2.0/repositories/{workspace}/{repo}/pullrequests/{id}. Everything after
// src/{commit} collapses to {path}, a branch or tag name keeps its slashes
// inside {name}, and a path under neither API root becomes /{path}.
func Template(path string) string {
	path, _, _ = strings.Cut(path, "?")
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if segments[0] != "2.0" && segments[0] != "rest" {
		return "/{path}"
	}
	out := make([]string, 0, len(segments))
	for i := 0; i < len(segments); i++ {
		segment := segments[i]
		if i > 0 && (segments[i-1] == "api" || segments[i-1] == "agile") {
			if apiVersions[segment] {
				out = append(out, segment)
			} else {
				out = append(out, "{value}")
			}
			continue
		}
		out = append(out, placeholder(segment))
		names, ok := namedSegments[segment]
		if !ok || i == 0 {
			continue
		}
		for _, name := range names {
			if i+1 >= len(segments) {
				break
			}
			i++
			if apiWords[segments[i]] && segment != "src" && segment != "branches" && segment != "tags" {
				out = append(out, segments[i])
				break
			}
			out = append(out, name)
		}
		if segment == "src" && i+1 < len(segments) {
			out = append(out, "{path}")
			break
		}
		if segment == "branches" || segment == "tags" {
			break
		}
	}
	return "/" + strings.Join(out, "/")
}

func placeholder(segment string) string {
	switch {
	case apiWords[segment]:
		return segment
	case numberPattern.MatchString(segment):
		return "{id}"
	case uuidPattern.MatchString(segment):
		return "{uuid}"
	case hashPattern.MatchString(segment) && strings.ContainsAny(segment, "0123456789"):
		return "{hash}"
	case issueKey.MatchString(segment):
		return "{key}"
	}
	return "{value}"
}
