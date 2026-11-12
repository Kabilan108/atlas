package bitbucket

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

// hugePRServer serves total pull requests, newest updated first, one hour
// apart from 2025-06-01, in pages of the requested pagelen.
func hugePRServer(t *testing.T, total int) (*Client, *apiRecorder) {
	t.Helper()
	newest := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	client, recorder, server := newRecordingClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		pageLen, _ := strconv.Atoi(r.URL.Query().Get("pagelen"))
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		var values []string
		for i := (page - 1) * pageLen; i < page*pageLen && i < total; i++ {
			updated := newest.Add(-time.Duration(i) * time.Hour).Format(time.RFC3339)
			values = append(values, fmt.Sprintf(`{"id":%d,"author":{"account_id":"557058:a%d"},"updated_on":%q}`, total-i, i%7, updated))
		}
		next := ""
		if page*pageLen < total {
			query := r.URL.Query()
			query.Set("page", strconv.Itoa(page+1))
			next = fmt.Sprintf(`,"next":"%s%s?%s"`, "http://"+r.Host, r.URL.Path, query.Encode())
		}
		writeJSON(w, http.StatusOK, `{"values":[`+strings.Join(values, ",")+`]`+next+`}`)
	})
	_ = server
	return client, recorder
}

func TestListPullRequestsNeverReadsAHugeSetWhole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		opts     *PRListOptions
		wantPRs  int
		wantReqs int
		wantMore bool
	}{
		{name: "default limit", opts: nil, wantPRs: MaxPRListLimit, wantReqs: MaxPRListLimit / prPageLen, wantMore: true},
		{name: "small limit", opts: &PRListOptions{Limit: 7}, wantPRs: 7, wantReqs: 1, wantMore: true},
		{name: "limit across pages", opts: &PRListOptions{Limit: 120}, wantPRs: 120, wantReqs: 3, wantMore: true},
		{name: "filter matching some", opts: &PRListOptions{Limit: 10, Author: "557058:a3"}, wantPRs: 10, wantReqs: 2, wantMore: true},
		{name: "filter matching none", opts: &PRListOptions{Author: "557058:nobody"}, wantPRs: 0, wantReqs: maxPRListScan / prPageLen, wantMore: true},
		{name: "updated since", opts: &PRListOptions{UpdatedSince: time.Date(2025, 5, 31, 0, 0, 0, 0, time.UTC)}, wantPRs: 25, wantReqs: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client, recorder := hugePRServer(t, 100000)
			list, err := client.ListPullRequests(ctx, "acme", "widgets", tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(list.PullRequests) != tc.wantPRs || list.More != tc.wantMore {
				t.Fatalf("got %d PRs, more %t; want %d, more %t", len(list.PullRequests), list.More, tc.wantPRs, tc.wantMore)
			}
			if got := len(recorder.all()); got != tc.wantReqs {
				t.Fatalf("made %d requests, want %d", got, tc.wantReqs)
			}
			if len(list.PullRequests) > 1 && list.PullRequests[0].UpdatedOn.Before(list.PullRequests[1].UpdatedOn) {
				t.Fatalf("not newest updated first: %v then %v", list.PullRequests[0].UpdatedOn, list.PullRequests[1].UpdatedOn)
			}
		})
	}
}

func TestListPullRequestsQuery(t *testing.T) {
	t.Parallel()
	client, recorder := hugePRServer(t, 3)
	since := time.Date(2025, 1, 2, 3, 4, 5, 0, time.FixedZone("EST", -5*3600))
	_, err := client.ListPullRequests(context.Background(), "acme", "widgets", &PRListOptions{
		States: []string{"OPEN", "MERGED"}, UpdatedSince: since, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	query := recorder.all()[0].Query
	if got := query["state"]; len(got) != 2 || got[0] != "OPEN" || got[1] != "MERGED" {
		t.Fatalf("state = %v", got)
	}
	if got := query.Get("q"); got != "updated_on >= 2025-01-02T08:04:05+00:00" {
		t.Fatalf("q = %q", got)
	}
	if query.Get("sort") != "-updated_on" || query.Get("pagelen") != "20" {
		t.Fatalf("query = %v", query)
	}
}
