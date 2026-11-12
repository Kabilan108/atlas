package jira

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/kabilan108/atlas/internal/ops/jira/jiratest"
)

// An issue named by key in one entry and by ID in another is one issue.
func TestWorklogAddDedupesKeyAndID(t *testing.T) {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/myself", http.StatusOK, `{"accountId":"acc-me"}`)
	logs := fake.IssueWorklogs("30756", "CSM-756", "acc-me")
	res, err := invoke(t, fake, WorklogAdd, `{"entries":[
		{"issue":"CSM-756","started":"2026-11-11T09:00:00-05:00","duration":"1h"},
		{"issue":"30756","started":"2026-11-11T14:00:00Z","duration":"60m"}]}`)
	if err != nil {
		t.Fatalf("%v\nrequests: %v", err, fake.Routes())
	}
	if logs.Posts() != 1 {
		t.Errorf("posted %d worklogs, want 1\n%s", logs.Posts(), res.Text)
	}
	wantContains(t, res.Text, `"index":0,"status":"ok","issue":"CSM-756"`, `"index":1,"status":"skipped","issue":"CSM-756"`)
}

// Jira stores starts to the millisecond, so starts within the same second
// are different worklogs; the same instant in another offset is not.
func TestWorklogAddComparesStartsToTheMillisecond(t *testing.T) {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/myself", http.StatusOK, `{"accountId":"acc-me"}`)
	logs := fake.IssueWorklogs("30756", "CSM-756", "acc-me")
	logs.Add("acc-me", "2026-11-11T14:00:00.900+0000", 3600)
	res, err := invoke(t, fake, WorklogAdd, `{"entries":[
		{"issue":"CSM-756","started":"2026-11-11T14:00:00.100Z","duration":"1h"},
		{"issue":"CSM-756","started":"2026-11-11T09:00:00.900-05:00","duration":"1h"}]}`)
	if err != nil {
		t.Fatalf("%v\nrequests: %v", err, fake.Routes())
	}
	wantContains(t, res.Text, `"index":0,"status":"ok"`, `"index":1,"status":"skipped","issue":"CSM-756","started":"2026-11-11T09:00:00.900-0500","duration":"1h","worklog_id":"900"`)
	if logs.Posts() != 1 {
		t.Errorf("posted %d worklogs, want 1", logs.Posts())
	}
}

// Two calls racing to log the same entry through one server post it once:
// the fake holds each worklog check until a second one arrives (or a
// timeout passes), so two unserialized calls both see no worklog.
func TestWorklogAddSerializesConcurrentCalls(t *testing.T) {
	fake := jiratest.New()
	fake.JSON("GET /rest/api/3/myself", http.StatusOK, `{"accountId":"acc-me"}`)
	logs := fake.IssueWorklogs("30756", "CSM-756", "acc-me")
	var mu sync.Mutex
	checks := 0
	both := make(chan struct{})
	logs.BeforeList = func() {
		mu.Lock()
		checks++
		if checks == 2 {
			close(both)
		}
		mu.Unlock()
		select {
		case <-both:
		case <-time.After(300 * time.Millisecond):
		}
	}
	env := newEnv(t, fake)
	args := json.RawMessage(`{"entries":[{"issue":"CSM-756","started":"2026-11-11T09:00:00-05:00","duration":"1h"}]}`)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = WorklogAdd.Invoke(context.Background(), env, args)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("call %d: %v", i, err)
		}
	}
	if logs.Posts() != 1 {
		t.Errorf("concurrent calls posted %d worklogs, want 1", logs.Posts())
	}
	if n := worklogLocks.size(); n != 0 {
		t.Errorf("%d worklog locks left after the calls", n)
	}
}

func TestKeyedLocks(t *testing.T) {
	var locks keyedLocks
	unlock, err := locks.lock(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	otherUnlock, err := locks.lock(context.Background(), "b")
	if err != nil {
		t.Fatalf("a different key waited: %v", err)
	}
	otherUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := locks.lock(ctx, "a"); err != context.DeadlineExceeded {
		t.Errorf("second lock on a held key = %v, want the deadline", err)
	}
	unlock()
	if n := locks.size(); n != 0 {
		t.Errorf("%d locks left after every holder unlocked", n)
	}
}
