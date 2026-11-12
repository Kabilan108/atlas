package jira

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// endless serves pages of n items from a set Jira says holds a million.
func endless(t *testing.T, key string, requests *int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*requests++
		startAt, _ := strconv.Atoi(r.URL.Query().Get("startAt"))
		size, _ := strconv.Atoi(r.URL.Query().Get("maxResults"))
		if size <= 0 || size > 1000 {
			t.Errorf("maxResults = %d", size)
		}
		items := make([]string, size)
		for i := range items {
			items[i] = fmt.Sprintf(`{"id":"%d","started":"2026-11-11T09:00:00.000-0500","timeSpentSeconds":60}`, startAt+i)
		}
		fmt.Fprintf(w, `{"startAt":%d,"maxResults":%d,"total":1000000,"%s":[%s]}`, startAt, size, key, strings.Join(items, ","))
	}
}

func TestWorklogsUpToStopsAtMax(t *testing.T) {
	requests := 0
	c := testClient(t, endless(t, "worklogs", &requests))
	got, total, err := c.WorklogsUpTo(ctx, "CSM-1", WorklogListOptions{}, 2500)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2500 || total != 1000000 || requests != 3 {
		t.Errorf("got %d of %d in %d requests", len(got), total, requests)
	}
	if _, _, err := c.WorklogsUpTo(ctx, "CSM-1", WorklogListOptions{}, 0); err == nil {
		t.Error("max 0 accepted")
	}
}

func TestChangelogNewestReadsOnlyTheTail(t *testing.T) {
	requests := 0
	var starts []string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		starts = append(starts, r.URL.Query().Get("startAt"))
		endless(t, "values", &requests)(w, r)
	})
	got, total, err := c.ChangelogNewest(ctx, "CSM-1", 150)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 150 || total != 1000000 || got[0].ID != "999850" || got[149].ID != "999999" {
		t.Errorf("got %d of %d, first %s", len(got), total, got[0].ID)
	}
	if strings.Join(starts, ",") != "0,999850,999950" {
		t.Errorf("startAt sequence = %v", starts)
	}
}

func TestChangelogNewestShortHistory(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"startAt":0,"maxResults":100,"total":2,"values":[{"id":"1"},{"id":"2"}]}`))
	})
	got, total, err := c.ChangelogNewest(ctx, "CSM-1", 500)
	if err != nil || len(got) != 2 || total != 2 {
		t.Fatalf("got %v, %d, %v", got, total, err)
	}
}
