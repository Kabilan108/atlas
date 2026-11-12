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

// versionedSegments are followed by an API version, which stays as is:
// /rest/api/3/...
var versionedSegments = map[string]bool{"api": true, "agile": true}

// Template drops the query and replaces names and IDs in path with
// placeholders, so calls that differ only in which object they name count
// as one: /2.0/repositories/acme/app/pullrequests/42 becomes
// /2.0/repositories/{workspace}/{repo}/pullrequests/{id}. Everything after
// src/{commit} collapses to {path}, and a branch or tag name keeps its
// slashes inside {name}.
func Template(path string) string {
	path, _, _ = strings.Cut(path, "?")
	segments := strings.Split(strings.Trim(path, "/"), "/")
	out := make([]string, 0, len(segments))
	for i := 0; i < len(segments); i++ {
		segment := segments[i]
		if i > 0 && versionedSegments[segments[i-1]] {
			out = append(out, segment)
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
	case numberPattern.MatchString(segment):
		return "{id}"
	case uuidPattern.MatchString(segment):
		return "{uuid}"
	case hashPattern.MatchString(segment) && strings.ContainsAny(segment, "0123456789"):
		return "{hash}"
	case issueKey.MatchString(segment):
		return "{key}"
	}
	return segment
}
