package bitbucket

import (
	"strings"
	"time"
)

type User struct {
	UUID        string `json:"uuid"`
	Username    string `json:"username"`
	Nickname    string `json:"nickname"`
	DisplayName string `json:"display_name"`
	AccountID   string `json:"account_id"`
	Links       Links  `json:"links"`
}

func (u User) IdentityKey() string {
	for _, value := range []string{u.UUID, u.AccountID, u.Username, u.Nickname, u.DisplayName} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (u User) Handle() string {
	for _, value := range []string{u.Username, u.Nickname, u.DisplayName, u.AccountID, strings.Trim(u.UUID, "{}")} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return "unknown"
}

func (u User) MatchesStableIdentifier(identifier string) bool {
	target := normalizeUserIdentifier(identifier)
	if target == "" {
		return false
	}

	for _, candidate := range u.stableIdentifiers() {
		if normalizeUserIdentifier(candidate) == target {
			return true
		}
	}

	return false
}

func (u User) SharesStableIdentity(other User) bool {
	for _, candidate := range u.stableIdentifiers() {
		if other.MatchesStableIdentifier(candidate) {
			return true
		}
	}
	return false
}

func (u User) stableIdentifiers() []string {
	return []string{
		u.UUID,
		strings.Trim(u.UUID, "{}"),
		u.AccountID,
		u.Username,
	}
}

func normalizeUserIdentifier(value string) string {
	normalized := strings.TrimSpace(value)
	normalized = strings.TrimPrefix(normalized, "@")
	normalized = strings.Trim(normalized, "{}")
	return strings.ToLower(normalized)
}

type Links struct {
	Self   Link `json:"self"`
	HTML   Link `json:"html"`
	Avatar Link `json:"avatar"`
}

type Link struct {
	Href string `json:"href"`
}

type Repository struct {
	UUID        string    `json:"uuid"`
	Name        string    `json:"name"`
	FullName    string    `json:"full_name"`
	Description string    `json:"description"`
	IsPrivate   bool      `json:"is_private"`
	Owner       User      `json:"owner"`
	MainBranch  Branch    `json:"mainbranch"`
	CreatedOn   time.Time `json:"created_on"`
	UpdatedOn   time.Time `json:"updated_on"`
	Links       Links     `json:"links"`
}

type PullRequest struct {
	ID           int              `json:"id"`
	Title        string           `json:"title"`
	Description  string           `json:"description"`
	State        string           `json:"state"`
	Author       User             `json:"author"`
	Source       PullRequestRef   `json:"source"`
	Destination  PullRequestRef   `json:"destination"`
	Reviewers    []User           `json:"reviewers"`
	Participants []Participant    `json:"participants"`
	CreatedOn    time.Time        `json:"created_on"`
	UpdatedOn    time.Time        `json:"updated_on"`
	Links        PullRequestLinks `json:"links"`
	CommentCount int              `json:"comment_count"`
	TaskCount    int              `json:"task_count"`
}

type PRListOptions struct {
	State    string
	Author   string
	Reviewer string
}

type PullRequestRef struct {
	Branch     Branch     `json:"branch"`
	Commit     Commit     `json:"commit"`
	Repository Repository `json:"repository"`
}

type PullRequestRefInput struct {
	Branch     Branch           `json:"branch"`
	Repository *RepositoryInput `json:"repository,omitempty"`
}

type RepositoryInput struct {
	FullName string `json:"full_name,omitempty"`
	UUID     string `json:"uuid,omitempty"`
}

type PullRequestCreate struct {
	Title       string              `json:"title"`
	Description string              `json:"description,omitempty"`
	Source      PullRequestRefInput `json:"source"`
	Destination PullRequestRefInput `json:"destination"`
	Reviewers   []User              `json:"reviewers,omitempty"`
}

type PullRequestUpdate struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	Reviewers   *[]User `json:"reviewers,omitempty"`
}

type Branch struct {
	Name string `json:"name"`
}

type Commit struct {
	Hash string `json:"hash"`
}

type Participant struct {
	User     User   `json:"user"`
	Role     string `json:"role"`
	Approved bool   `json:"approved"`
	State    string `json:"state"`
}

type PullRequestLinks struct {
	Self     Link `json:"self"`
	HTML     Link `json:"html"`
	Commits  Link `json:"commits"`
	Approve  Link `json:"approve"`
	Diff     Link `json:"diff"`
	Comments Link `json:"comments"`
}

type Comment struct {
	ID         int         `json:"id"`
	Content    Content     `json:"content"`
	User       User        `json:"user"`
	CreatedOn  time.Time   `json:"created_on"`
	UpdatedOn  time.Time   `json:"updated_on"`
	Inline     *Inline     `json:"inline,omitempty"`
	Parent     *Parent     `json:"parent,omitempty"`
	Deleted    bool        `json:"deleted"`
	Pending    bool        `json:"pending"`
	Resolution *Resolution `json:"resolution,omitempty"`
	Links      Links       `json:"links"`
}

type CommentCreate struct {
	Content ContentInput `json:"content"`
	Inline  *InlineInput `json:"inline,omitempty"`
	Parent  *ParentInput `json:"parent,omitempty"`
}

type Resolution struct {
	User User      `json:"user"`
	Date time.Time `json:"date"`
}

func (c *Comment) IsResolved() bool {
	return c.Resolution != nil
}

type Content struct {
	Raw    string `json:"raw"`
	Markup string `json:"markup"`
	HTML   string `json:"html"`
}

type ContentInput struct {
	Raw string `json:"raw"`
}

type Inline struct {
	Path string `json:"path"`
	From *int   `json:"from,omitempty"`
	To   *int   `json:"to,omitempty"`
}

type InlineInput struct {
	Path string `json:"path"`
	From *int   `json:"from,omitempty"`
	To   *int   `json:"to,omitempty"`
}

type Parent struct {
	ID int `json:"id"`
}

type ParentInput struct {
	ID int `json:"id"`
}

type ReviewActionResult struct {
	Type           string    `json:"type"`
	User           User      `json:"user"`
	Role           string    `json:"role"`
	Approved       bool      `json:"approved"`
	State          string    `json:"state"`
	ParticipatedOn time.Time `json:"participated_on"`
}

type PaginatedResponse[T any] struct {
	Size     int    `json:"size"`
	Page     int    `json:"page"`
	PageLen  int    `json:"pagelen"`
	Next     string `json:"next,omitempty"`
	Previous string `json:"previous,omitempty"`
	Values   []T    `json:"values"`
}

type Task struct {
	ID      int     `json:"id"`
	Content Content `json:"content"`
	State   string  `json:"state"`
	Comment Comment `json:"comment"`
}

func (t *Task) IsResolved() bool {
	return t.State == "RESOLVED"
}

type Snippet struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	IsPrivate bool                   `json:"is_private"`
	Owner     User                   `json:"owner"`
	Files     map[string]SnippetFile `json:"files"`
	CreatedOn time.Time              `json:"created_on"`
	UpdatedOn time.Time              `json:"updated_on"`
	Links     Links                  `json:"links"`
}

type SnippetFile struct {
	Links Links `json:"links"`
}
