package jiratest

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Worklogs is one issue's worklog list, served under both the issue's key
// and its ID like Jira does.
type Worklogs struct {
	// BeforeList, when set, runs at the start of every worklog GET, before
	// the list is read, so a test can hold concurrent callers at that point.
	BeforeList func()

	mu      sync.Mutex
	entries []worklogEntry
	posts   int
}

type worklogEntry struct {
	ID               string          `json:"id"`
	Author           json.RawMessage `json:"author"`
	Started          string          `json:"started"`
	TimeSpentSeconds int             `json:"timeSpentSeconds"`
}

// IssueWorklogs registers GET /rest/api/3/issue/{key} and /{id} (the issue
// with a summary) and GET and POST on their /worklog. POSTs are logged as
// account, the caller's account ID. GET honors startedAfter and
// startedBefore.
func (f *Fake) IssueWorklogs(id, key, account string) *Worklogs {
	store := &Worklogs{}
	issue := fmt.Sprintf(`{"id":%q,"key":%q,"fields":{"summary":"s"}}`, id, key)
	for _, ref := range []string{id, key} {
		f.JSON("GET /rest/api/3/issue/"+ref, http.StatusOK, issue)
		f.Handle("GET /rest/api/3/issue/"+ref+"/worklog", store.list)
		f.Handle("POST /rest/api/3/issue/"+ref+"/worklog", func(w http.ResponseWriter, r *http.Request) {
			var req worklogEntry
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				Write(w, http.StatusBadRequest, `{"errorMessages":["jiratest: bad worklog body"]}`)
				return
			}
			logged := store.Add(account, req.Started, req.TimeSpentSeconds)
			store.mu.Lock()
			store.posts++
			store.mu.Unlock()
			Write(w, http.StatusCreated, fmt.Sprintf(`{"id":%q}`, logged))
		})
	}
	return store
}

// Add records a worklog by account and returns its ID.
func (s *Worklogs) Add(account, started string, seconds int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := strconv.Itoa(900 + len(s.entries))
	author, _ := json.Marshal(map[string]string{"accountId": account})
	s.entries = append(s.entries, worklogEntry{ID: id, Author: author, Started: started, TimeSpentSeconds: seconds})
	return id
}

// Posts is how many worklogs were POSTed.
func (s *Worklogs) Posts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.posts
}

func (s *Worklogs) list(w http.ResponseWriter, r *http.Request) {
	if s.BeforeList != nil {
		s.BeforeList()
	}
	q := r.URL.Query()
	after, _ := strconv.ParseInt(q.Get("startedAfter"), 10, 64)
	before, _ := strconv.ParseInt(q.Get("startedBefore"), 10, 64)
	s.mu.Lock()
	shown := []worklogEntry{}
	for _, e := range s.entries {
		started, err := time.Parse("2006-01-02T15:04:05.000-0700", e.Started)
		if err != nil {
			continue
		}
		ms := started.UnixMilli()
		if (after > 0 && ms < after) || (before > 0 && ms > before) {
			continue
		}
		shown = append(shown, e)
	}
	s.mu.Unlock()
	data, _ := json.Marshal(map[string]any{"startAt": 0, "maxResults": 1000, "total": len(shown), "worklogs": shown})
	Write(w, http.StatusOK, string(data))
}
