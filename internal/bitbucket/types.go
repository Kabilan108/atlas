package bitbucket

import "time"

type User struct {
	UUID        string `json:"uuid"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	AccountID   string `json:"account_id"`
	Links       Links  `json:"links"`
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
	ID         int          `json:"id"`
	Content    Content      `json:"content"`
	User       User         `json:"user"`
	CreatedOn  time.Time    `json:"created_on"`
	UpdatedOn  time.Time    `json:"updated_on"`
	Inline     *Inline      `json:"inline,omitempty"`
	Parent     *Parent      `json:"parent,omitempty"`
	Deleted    bool         `json:"deleted"`
	Pending    bool         `json:"pending"`
	Resolution *Resolution  `json:"resolution,omitempty"`
	Links      Links        `json:"links"`
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

type Inline struct {
	Path string `json:"path"`
	From *int   `json:"from,omitempty"`
	To   *int   `json:"to,omitempty"`
}

type Parent struct {
	ID int `json:"id"`
}

type PaginatedResponse[T any] struct {
	Size     int    `json:"size"`
	Page     int    `json:"page"`
	PageLen  int    `json:"pagelen"`
	Next     string `json:"next,omitempty"`
	Previous string `json:"previous,omitempty"`
	Values   []T    `json:"values"`
}
