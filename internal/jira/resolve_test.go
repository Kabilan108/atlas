package jira

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

var createFields = []FieldMeta{
	{FieldID: "summary", Name: "Summary", Required: true, Schema: FieldSchema{Type: "string", System: "summary"}},
	{FieldID: "description", Name: "Description", Schema: FieldSchema{Type: "string", System: "description"}},
	{FieldID: "customfield_10153", Name: "Product", Schema: FieldSchema{Type: "array", Items: "option", Custom: "com.atlassian.jira.plugin.system.customfieldtypes:multiselect", CustomID: 10153},
		AllowedValues: []AllowedValue{{ID: "10200", Value: "Atlas"}, {ID: "10201", Value: "Beacon"}, {ID: "10202", Value: "Retired", Disabled: true}}},
	{FieldID: "customfield_10469", Name: "Software Version", Schema: FieldSchema{Type: "string", Custom: "com.atlassian.jira.plugin.system.customfieldtypes:textfield"}},
	{FieldID: "customfield_10500", Name: "Team", Schema: FieldSchema{Type: "option", Custom: "com.atlassian.jira.plugin.system.customfieldtypes:select"},
		AllowedValues: []AllowedValue{{ID: "1", Value: "Platform"}, {ID: "2", Value: "platform"}, {ID: "3", Value: "Apps"}}},
	{FieldID: "customfield_10600", Name: "team", Schema: FieldSchema{Type: "string"}},
	{FieldID: "customfield_10700", Name: "Region", Schema: FieldSchema{Type: "string"}},
	{FieldID: "customfield_10701", Name: "region", Schema: FieldSchema{Type: "string"}},
	{FieldID: "customfield_10702", Name: "REGION", Schema: FieldSchema{Type: "string"}},
	{FieldID: "customfield_10800", Name: "Story Points", Schema: FieldSchema{Type: "number"}},
	{FieldID: "customfield_10900", Name: "Reviewers", Schema: FieldSchema{Type: "array", Items: "user"}},
	{FieldID: "customfield_11000", Name: "Steps", Schema: FieldSchema{Type: "string", Custom: "com.atlassian.jira.plugin.system.customfieldtypes:textarea"}},
	{FieldID: "assignee", Name: "Assignee", Schema: FieldSchema{Type: "user", System: "assignee"}},
	{FieldID: "priority", Name: "Priority", Schema: FieldSchema{Type: "priority", System: "priority"}, AllowedValues: []AllowedValue{{ID: "1", Name: "Highest"}, {ID: "3", Name: "Medium"}}},
	{FieldID: "labels", Name: "Labels", Schema: FieldSchema{Type: "array", Items: "string", System: "labels"}},
	{FieldID: "parent", Name: "Parent", Schema: FieldSchema{Type: "issuelink", System: "parent"}},
	{FieldID: "fixVersions", Name: "Fix versions", Schema: FieldSchema{Type: "array", Items: "version", System: "fixVersions"}},
	{FieldID: "issuetype", Name: "Issue Type", Schema: FieldSchema{Type: "issuetype", System: "issuetype"}},
}

func TestResolveField(t *testing.T) {
	tests := []struct {
		name, input, wantID string
		ambiguous           []string
		missing             bool
	}{
		{name: "display name", input: "Product", wantID: "customfield_10153"},
		{name: "case-insensitive", input: "software version", wantID: "customfield_10469"},
		{name: "field id", input: "customfield_10153", wantID: "customfield_10153"},
		{name: "exact beats case-insensitive", input: "Team", wantID: "customfield_10500"},
		{name: "exact lower beats other case", input: "team", wantID: "customfield_10600"},
		{name: "ambiguous case-insensitive", input: "rEgIoN", ambiguous: []string{"Region (customfield_10700)", "region (customfield_10701)", "REGION (customfield_10702)"}},
		{name: "trimmed", input: "  Story Points ", wantID: "customfield_10800"},
		{name: "missing", input: "Sprint", missing: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveField(createFields, tc.input)
			var re *ResolveError
			switch {
			case tc.ambiguous != nil:
				if !errors.As(err, &re) || !re.Ambiguous || !reflect.DeepEqual(re.Candidates, tc.ambiguous) {
					t.Fatalf("err = %v, want ambiguity listing %v", err, tc.ambiguous)
				}
			case tc.missing:
				if !errors.As(err, &re) || re.Ambiguous || !strings.Contains(err.Error(), "Product") || !strings.Contains(err.Error(), "Summary") {
					t.Fatalf("err = %v, want miss listing field names", err)
				}
			default:
				if err != nil || got.ID() != tc.wantID {
					t.Fatalf("ResolveField(%q) = %q, %v; want %q", tc.input, got.ID(), err, tc.wantID)
				}
			}
		})
	}
}

var transitions = []Transition{
	{ID: "11", Name: "Start Progress", To: Status{ID: "3", Name: "In Progress"}},
	{ID: "21", Name: "Resolve Issue", To: Status{ID: "5", Name: "Resolved (2)"}},
	{ID: "31", Name: "Close", To: Status{ID: "6", Name: "Closed"}},
	{ID: "41", Name: "Reopen", To: Status{ID: "4", Name: "Open"}},
	{ID: "51", Name: "Back to open", To: Status{ID: "4", Name: "Open"}},
}

func TestResolveTransition(t *testing.T) {
	tests := []struct {
		input, wantID string
		wantErr       string
	}{
		{input: "start progress", wantID: "11"},
		{input: "In Progress", wantID: "11"},
		{input: "Resolved", wantID: "21"},
		{input: "resolved (2)", wantID: "21"},
		{input: "Resolve Issue", wantID: "21"},
		{input: "31", wantID: "31"},
		{input: "CLOSED", wantID: "31"},
		{input: "reopen", wantID: "41"},
		{input: "open", wantErr: "ambiguous"},
		{input: "Done", wantErr: "valid: Back to open (to Open), Close (to Closed), Reopen (to Open), Resolve Issue (to Resolved (2)), Start Progress (to In Progress)"},
	}
	for _, tc := range tests {
		got, err := ResolveTransition(transitions, tc.input)
		if tc.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("ResolveTransition(%q) err = %v, want %q", tc.input, err, tc.wantErr)
			}
			continue
		}
		if err != nil || got.ID != tc.wantID {
			t.Errorf("ResolveTransition(%q) = %q, %v; want %q", tc.input, got.ID, err, tc.wantID)
		}
	}
}

func TestResolveTransitionNameBeatsStatus(t *testing.T) {
	ts := []Transition{
		{ID: "1", Name: "Done", To: Status{Name: "Closed"}},
		{ID: "2", Name: "Finish", To: Status{Name: "Done"}},
	}
	got, err := ResolveTransition(ts, "done")
	if err != nil || got.ID != "1" {
		t.Errorf("got %q, %v; want transition named Done", got.ID, err)
	}
}

func TestResolveLinkType(t *testing.T) {
	types := []IssueLinkType{
		{ID: "1000", Name: "Duplicate", Inward: "is duplicated by", Outward: "duplicates"},
		{ID: "1010", Name: "2 Relates", Inward: "relates to", Outward: "relates to"},
		{ID: "1020", Name: "10 - Blocks", Inward: "is blocked by", Outward: "blocks"},
	}
	tests := []struct{ input, wantID string }{
		{"Relates", "1010"},
		{"relates", "1010"},
		{"2 relates", "1010"},
		{"Blocks", "1020"},
		{"duplicate", "1000"},
		{"1020", "1020"},
	}
	for _, tc := range tests {
		got, err := ResolveLinkType(types, tc.input)
		if err != nil || got.ID != tc.wantID {
			t.Errorf("ResolveLinkType(%q) = %q, %v; want %q", tc.input, got.ID, err, tc.wantID)
		}
	}
	_, err := ResolveLinkType(types, "Clones")
	if err == nil || !strings.Contains(err.Error(), "valid: 10 - Blocks, 2 Relates, Duplicate") {
		t.Errorf("miss err = %v", err)
	}
}

func field(t *testing.T, name string) FieldMeta {
	t.Helper()
	f, err := ResolveField(createFields, name)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestShapeFieldValue(t *testing.T) {
	tests := []struct {
		field string
		value any
		want  string
	}{
		{"Product", []any{"atlas", "Beacon"}, `[{"value":"Atlas"},{"value":"Beacon"}]`},
		{"Product", "Atlas", `[{"value":"Atlas"}]`},
		{"Product", []string{"BEACON"}, `[{"value":"Beacon"}]`},
		{"Team", "Apps", `{"value":"Apps"}`},
		{"Team", "platform", `{"value":"platform"}`},
		{"Software Version", "1.4.2", `"1.4.2"`},
		{"Story Points", "5", `5`},
		{"Story Points", 3.5, `3.5`},
		{"Assignee", "5b10a2844c20165700ede21g", `{"accountId":"5b10a2844c20165700ede21g"}`},
		{"Reviewers", []any{"acc-1", "acc-2"}, `[{"accountId":"acc-1"},{"accountId":"acc-2"}]`},
		{"Priority", "medium", `{"name":"Medium"}`},
		{"Labels", []any{"auth", "staging"}, `["auth","staging"]`},
		{"Parent", "ED-0", `{"key":"ED-0"}`},
		{"Fix versions", "1.5", `[{"name":"1.5"}]`},
		{"Issue Type", "10001", `{"id":"10001"}`},
		{"Issue Type", "Bug", `{"name":"Bug"}`},
		{"Team", map[string]any{"id": "3"}, `{"id":"3"}`},
		{"Description", "Fixed in **v2**", `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Fixed in "},{"type":"text","text":"v2","marks":[{"type":"strong"}]}]}]}`},
		{"Steps", "1. run", `{"type":"doc","version":1,"content":[{"type":"orderedList","content":[{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"run"}]}]}]}]}`},
	}
	for _, tc := range tests {
		got, err := ShapeFieldValue(field(t, tc.field), tc.value)
		if err != nil {
			t.Errorf("ShapeFieldValue(%s, %v) err = %v", tc.field, tc.value, err)
			continue
		}
		jsonEqual(t, got, tc.want)
	}
}

func TestShapeFieldValueErrors(t *testing.T) {
	tests := []struct {
		field   string
		value   any
		wantErr string
	}{
		{"Product", "Gamma", `valid: Atlas, Beacon`},
		{"Product", "Retired", `no value for Product "Retired"`},
		{"Team", "PLATFORM", `ambiguous; matches: Platform, platform`},
		{"Team", []any{"Apps", "Platform"}, "takes one value, got 2"},
		{"Story Points", "lots", `"lots" is not a number`},
		{"Priority", "Low", `valid: Highest, Medium`},
	}
	for _, tc := range tests {
		_, err := ShapeFieldValue(field(t, tc.field), tc.value)
		if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("ShapeFieldValue(%s, %v) err = %v, want %q", tc.field, tc.value, err, tc.wantErr)
		}
	}
}

func TestParseAndFormatTime(t *testing.T) {
	tests := []struct{ in, want string }{
		{"2026-11-11T09:30:00-04:00", "2026-11-11T09:30:00.000-0400"},
		{"2026-11-11T13:30:00Z", "2026-11-11T13:30:00.000+0000"},
		{"2026-11-11T09:30:00.250-04:00", "2026-11-11T09:30:00.250-0400"},
		{"2026-11-11T09:30:00.000-0400", "2026-11-11T09:30:00.000-0400"},
		{"2026-11-11T09:30:00-0400", "2026-11-11T09:30:00.000-0400"},
		{"2026-11-11T09:30-04:00", "2026-11-11T09:30:00.000-0400"},
		{"2026-11-11T09:30+0530", "2026-11-11T09:30:00.000+0530"},
		{" 2026-11-11T09:30:00+01:00 ", "2026-11-11T09:30:00.000+0100"},
	}
	for _, tc := range tests {
		got, err := NormalizeTime(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("NormalizeTime(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	for _, bad := range []string{"2026-11-11T09:30:00", "2026-11-11", "yesterday", ""} {
		if _, err := NormalizeTime(bad); err == nil {
			t.Errorf("NormalizeTime(%q) = nil error", bad)
		}
	}
	a, _ := ParseTime("2026-11-11T09:30:00-04:00")
	b, _ := ParseTime("2026-11-11T13:30:00.000+0000")
	if !a.Equal(b) {
		t.Errorf("same instant parsed differently: %v vs %v", a, b)
	}
	if got := FormatTime(time.Date(2026, 1, 2, 3, 4, 5, 6e6, time.FixedZone("", -5*3600))); got != "2026-01-02T03:04:05.006-0500" {
		t.Errorf("FormatTime = %q", got)
	}
}

func TestShapeFieldValueNilClears(t *testing.T) {
	for _, name := range []string{"Product", "Team", "Software Version", "Story Points", "Assignee", "Reviewers", "Priority", "Labels", "Parent", "Fix versions", "Issue Type", "Description"} {
		got, err := ShapeFieldValue(field(t, name), nil)
		if err != nil {
			t.Errorf("ShapeFieldValue(%s, nil) err = %v", name, err)
			continue
		}
		jsonEqual(t, map[string]any{"f": got}, `{"f":null}`)
	}
	if _, err := ShapeFieldValue(field(t, "Reviewers"), []any{"acc-1", nil}); err == nil {
		t.Error("nil element in an array = nil error")
	}
}
