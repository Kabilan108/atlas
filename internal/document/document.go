package document

import "time"

// Document standardises the fields emitted by the CLI output formatters.
type Document struct {
	Title     string
	URL       string
	ID        string
	Source    string
	Space     string
	Workspace string
	Repo      string
	Path      string
	Author    string
	UpdatedAt time.Time
	Body      string
	Diff      string
}
