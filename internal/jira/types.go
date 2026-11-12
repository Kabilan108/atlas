package jira

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Timestamps are kept as Jira sends them (2006-01-02T15:04:05.000-0700); use
// ParseTime to read one.

type User struct {
	Self         string `json:"self,omitempty"`
	AccountID    string `json:"accountId"`
	AccountType  string `json:"accountType,omitempty"`
	EmailAddress string `json:"emailAddress,omitempty"`
	DisplayName  string `json:"displayName,omitempty"`
	Active       bool   `json:"active"`
	TimeZone     string `json:"timeZone,omitempty"`
}

type StatusCategory struct {
	ID   int    `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

type Status struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	StatusCategory *StatusCategory `json:"statusCategory,omitempty"`
}

type IssueType struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Description    string `json:"description,omitempty"`
	Subtask        bool   `json:"subtask"`
	HierarchyLevel int    `json:"hierarchyLevel"`
}

type Priority struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Resolution struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type NamedRef struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name"`
}

type ProjectRef struct {
	ID             string `json:"id"`
	Key            string `json:"key"`
	Name           string `json:"name"`
	ProjectTypeKey string `json:"projectTypeKey,omitempty"`
}

// IssueRef is an issue embedded in another issue (parent, links) with a
// handful of its fields.
type IssueRef struct {
	ID     string `json:"id"`
	Key    string `json:"key"`
	Self   string `json:"self,omitempty"`
	Fields struct {
		Summary   string     `json:"summary"`
		Status    *Status    `json:"status,omitempty"`
		Priority  *Priority  `json:"priority,omitempty"`
		IssueType *IssueType `json:"issuetype,omitempty"`
	} `json:"fields"`
}

type IssueLinkType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
	Self    string `json:"self,omitempty"`
}

type IssueLink struct {
	ID           string        `json:"id"`
	Type         IssueLinkType `json:"type"`
	InwardIssue  *IssueRef     `json:"inwardIssue,omitempty"`
	OutwardIssue *IssueRef     `json:"outwardIssue,omitempty"`
}

type Attachment struct {
	ID        string `json:"id"`
	Self      string `json:"self,omitempty"`
	Filename  string `json:"filename"`
	Author    *User  `json:"author,omitempty"`
	Created   string `json:"created"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mimeType"`
	Content   string `json:"content"`
	Thumbnail string `json:"thumbnail,omitempty"`
}

// IssueFields decodes the system fields atlas reads. Every field, custom ones
// included, is also on Issue.RawFields.
type IssueFields struct {
	Summary        string          `json:"summary"`
	Description    json.RawMessage `json:"description,omitempty"`
	Status         *Status         `json:"status,omitempty"`
	IssueType      *IssueType      `json:"issuetype,omitempty"`
	Project        *ProjectRef     `json:"project,omitempty"`
	Assignee       *User           `json:"assignee,omitempty"`
	Reporter       *User           `json:"reporter,omitempty"`
	Creator        *User           `json:"creator,omitempty"`
	Priority       *Priority       `json:"priority,omitempty"`
	Resolution     *Resolution     `json:"resolution,omitempty"`
	Parent         *IssueRef       `json:"parent,omitempty"`
	Labels         []string        `json:"labels,omitempty"`
	Components     []NamedRef      `json:"components,omitempty"`
	FixVersions    []NamedRef      `json:"fixVersions,omitempty"`
	IssueLinks     []IssueLink     `json:"issuelinks,omitempty"`
	Attachments    []Attachment    `json:"attachment,omitempty"`
	Comment        *CommentPage    `json:"comment,omitempty"`
	Created        string          `json:"created,omitempty"`
	Updated        string          `json:"updated,omitempty"`
	ResolutionDate string          `json:"resolutiondate,omitempty"`
	DueDate        string          `json:"duedate,omitempty"`
}

type ChangelogItem struct {
	Field      string `json:"field"`
	FieldType  string `json:"fieldtype"`
	FieldID    string `json:"fieldId,omitempty"`
	From       string `json:"from"`
	FromString string `json:"fromString"`
	To         string `json:"to"`
	ToString   string `json:"toString"`
}

type ChangelogHistory struct {
	ID      string          `json:"id"`
	Author  *User           `json:"author,omitempty"`
	Created string          `json:"created"`
	Items   []ChangelogItem `json:"items"`
}

type Changelog struct {
	StartAt    int                `json:"startAt"`
	MaxResults int                `json:"maxResults"`
	Total      int                `json:"total"`
	Histories  []ChangelogHistory `json:"histories"`
}

// Issue keeps every field raw in RawFields (serialized as "fields") and the
// common system fields decoded in Fields.
type Issue struct {
	ID             string                     `json:"id"`
	Key            string                     `json:"key"`
	Self           string                     `json:"self,omitempty"`
	Fields         IssueFields                `json:"-"`
	RawFields      map[string]json.RawMessage `json:"fields,omitempty"`
	Names          map[string]string          `json:"names,omitempty"`
	Schema         map[string]FieldSchema     `json:"schema,omitempty"`
	RenderedFields map[string]json.RawMessage `json:"renderedFields,omitempty"`
	Changelog      *Changelog                 `json:"changelog,omitempty"`
}

func (i *Issue) UnmarshalJSON(data []byte) error {
	type plain Issue
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	var wire struct {
		Fields json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if len(wire.Fields) > 0 && string(wire.Fields) != "null" {
		if err := json.Unmarshal(wire.Fields, &p.Fields); err != nil {
			return err
		}
	}
	*i = Issue(p)
	return nil
}

// FieldByName finds a field by ID or display name using the names expansion,
// with ResolveField's rules: an ID, then an exact name, then a
// case-insensitive name. It returns the field ID and raw value, or a
// *ResolveError when nothing or more than one field matches.
func (i *Issue) FieldByName(name string) (string, json.RawMessage, error) {
	name = strings.TrimSpace(name)
	ids := slices.Sorted(maps.Keys(i.Names))
	valid := make([]string, len(ids))
	for n, id := range ids {
		valid[n] = i.Names[id]
	}
	id, err := pick("field", name, ids,
		func(id string) string { return id },
		func(id string) string { return fmt.Sprintf("%s (%s)", i.Names[id], id) },
		valid,
		func(id string) bool { return id == name },
		func(id string) bool { return i.Names[id] == name },
		func(id string) bool { return strings.EqualFold(i.Names[id], name) },
	)
	if err != nil {
		return "", nil, err
	}
	return id, i.RawFields[id], nil
}

type FieldSchema struct {
	Type     string `json:"type"`
	Items    string `json:"items,omitempty"`
	System   string `json:"system,omitempty"`
	Custom   string `json:"custom,omitempty"`
	CustomID int64  `json:"customId,omitempty"`
}

// AllowedValue covers the shapes Jira uses for allowed values: options
// ({id, value}), named objects ({id, name}) and projects ({id, key, name}).
type AllowedValue struct {
	ID       string         `json:"id,omitempty"`
	Value    string         `json:"value,omitempty"`
	Name     string         `json:"name,omitempty"`
	Key      string         `json:"key,omitempty"`
	Disabled bool           `json:"disabled,omitempty"`
	Children []AllowedValue `json:"children,omitempty"`
}

// Label is the text a user would type for this value.
func (v AllowedValue) Label() string {
	switch {
	case v.Value != "":
		return v.Value
	case v.Name != "":
		return v.Name
	case v.Key != "":
		return v.Key
	}
	return v.ID
}

// FieldMeta describes one field on a create, edit or transition screen.
type FieldMeta struct {
	FieldID         string          `json:"fieldId,omitempty"`
	Key             string          `json:"key,omitempty"`
	Name            string          `json:"name"`
	Required        bool            `json:"required"`
	Schema          FieldSchema     `json:"schema"`
	AllowedValues   []AllowedValue  `json:"allowedValues,omitempty"`
	HasDefaultValue bool            `json:"hasDefaultValue,omitempty"`
	DefaultValue    json.RawMessage `json:"defaultValue,omitempty"`
	Operations      []string        `json:"operations,omitempty"`
	AutoCompleteURL string          `json:"autoCompleteUrl,omitempty"`
}

// ID is the field ID to use in request bodies.
func (f FieldMeta) ID() string {
	if f.FieldID != "" {
		return f.FieldID
	}
	return f.Key
}

type Transition struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	To            Status               `json:"to"`
	HasScreen     bool                 `json:"hasScreen"`
	IsGlobal      bool                 `json:"isGlobal"`
	IsInitial     bool                 `json:"isInitial"`
	IsAvailable   bool                 `json:"isAvailable"`
	IsConditional bool                 `json:"isConditional"`
	Fields        map[string]FieldMeta `json:"fields,omitempty"`
}

type Visibility struct {
	Type       string `json:"type"`
	Value      string `json:"value,omitempty"`
	Identifier string `json:"identifier,omitempty"`
}

type Comment struct {
	ID           string          `json:"id"`
	Self         string          `json:"self,omitempty"`
	Author       *User           `json:"author,omitempty"`
	UpdateAuthor *User           `json:"updateAuthor,omitempty"`
	Body         json.RawMessage `json:"body,omitempty"`
	RenderedBody string          `json:"renderedBody,omitempty"`
	Created      string          `json:"created"`
	Updated      string          `json:"updated,omitempty"`
	Visibility   *Visibility     `json:"visibility,omitempty"`
	// JSDPublic is set on Service Management issues only; nil means Jira did
	// not say, not that the comment is internal.
	JSDPublic  *bool            `json:"jsdPublic,omitempty"`
	Properties []EntityProperty `json:"properties,omitempty"`
}

type EntityProperty struct {
	Key   string          `json:"key"`
	Value json.RawMessage `json:"value"`
}

type CommentPage struct {
	StartAt    int       `json:"startAt"`
	MaxResults int       `json:"maxResults"`
	Total      int       `json:"total"`
	Comments   []Comment `json:"comments"`
}

type Worklog struct {
	ID               string          `json:"id"`
	Self             string          `json:"self,omitempty"`
	IssueID          string          `json:"issueId,omitempty"`
	Author           *User           `json:"author,omitempty"`
	UpdateAuthor     *User           `json:"updateAuthor,omitempty"`
	Comment          json.RawMessage `json:"comment,omitempty"`
	Started          string          `json:"started"`
	TimeSpent        string          `json:"timeSpent"`
	TimeSpentSeconds int             `json:"timeSpentSeconds"`
	Created          string          `json:"created,omitempty"`
	Updated          string          `json:"updated,omitempty"`
	Visibility       *Visibility     `json:"visibility,omitempty"`
}

type Project struct {
	ID              string `json:"id"`
	Key             string `json:"key"`
	Name            string `json:"name"`
	Self            string `json:"self,omitempty"`
	ProjectTypeKey  string `json:"projectTypeKey"`
	Simplified      bool   `json:"simplified"`
	Style           string `json:"style,omitempty"`
	Description     string `json:"description,omitempty"`
	Lead            *User  `json:"lead,omitempty"`
	ProjectCategory *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"projectCategory,omitempty"`
	IssueTypes []IssueType `json:"issueTypes,omitempty"`
}

type ServiceDesk struct {
	ID          string `json:"id"`
	ProjectID   string `json:"projectId"`
	ProjectKey  string `json:"projectKey"`
	ProjectName string `json:"projectName"`
}

type ServiceDeskDate struct {
	ISO8601     string `json:"iso8601"`
	Jira        string `json:"jira"`
	Friendly    string `json:"friendly"`
	EpochMillis int64  `json:"epochMillis"`
}

type ServiceDeskUser struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress,omitempty"`
	Active       bool   `json:"active"`
	TimeZone     string `json:"timeZone,omitempty"`
}

// RequestComment is a Service Management request comment. Body is plain text
// (Jira wiki markup), not ADF.
type RequestComment struct {
	ID           string `json:"id"`
	Body         string `json:"body"`
	RenderedBody *struct {
		HTML string `json:"html"`
	} `json:"renderedBody,omitempty"`
	Public  bool             `json:"public"`
	Author  *ServiceDeskUser `json:"author,omitempty"`
	Created *ServiceDeskDate `json:"created,omitempty"`
}

type ServerInfo struct {
	BaseURL        string `json:"baseUrl"`
	Version        string `json:"version"`
	DeploymentType string `json:"deploymentType"`
	ServerTitle    string `json:"serverTitle"`
}
