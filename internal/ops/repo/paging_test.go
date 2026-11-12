package repo_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/repo"
)

// pagedEnv serves a total-item list at every path, page by page, and counts
// the requests.
func pagedEnv(t *testing.T, total int, item func(i int) string, requests *atomic.Int64) ops.Env {
	t.Helper()
	return transportEnv(t, func(r *http.Request) (int, io.ReadCloser) {
		requests.Add(1)
		query := r.URL.Query()
		page, _ := strconv.Atoi(query.Get("page"))
		page = max(page, 1)
		size, _ := strconv.Atoi(query.Get("pagelen"))
		if size < 1 {
			size = 10
		}
		start := (page - 1) * size
		var values []string
		for i := start; i < min(start+size, total); i++ {
			values = append(values, item(i))
		}
		body := `{"values":[` + strings.Join(values, ",") + `]`
		if start+size < total {
			next := *r.URL
			query.Set("page", strconv.Itoa(page+1))
			next.RawQuery = query.Encode()
			body += `,"next":"` + next.String() + `"`
		}
		return http.StatusOK, io.NopCloser(strings.NewReader(body + "}"))
	})
}

const hugeList = 30000

func namedItem(i int) string {
	n := strconv.Itoa(i)
	return `{"name":"item-` + n + `","full_name":"acme/item-` + n + `","user":{"display_name":"Member ` + n + `","account_id":"acct-` + n + `"},"hash":"abc` + n + `"}`
}

func TestListLimitsHaveHardMaxima(t *testing.T) {
	for _, tc := range []struct {
		def  ops.Def
		args string
	}{
		{repo.List, `{"limit":1000000000}`},
		{repo.BranchList, `{"repo":"acme/widgets","limit":1000000000}`},
		{repo.CommitList, `{"repo":"acme/widgets","limit":9223372036854775807}`},
		{repo.UserSearch, `{"query":"member","limit":1000}`},
	} {
		var requests atomic.Int64
		_, err := tc.def.Invoke(context.Background(), pagedEnv(t, hugeList, namedItem, &requests), json.RawMessage(tc.args))
		if ops.KindOf(err) != ops.Invalid || requests.Load() != 0 {
			t.Errorf("%s %s: err = %v after %d requests, want Invalid before any request", tc.def.Info().Name, tc.args, err, requests.Load())
		}
	}
}

func TestListsStopPagingAtTheirMaximum(t *testing.T) {
	for _, tc := range []struct {
		def          ops.Def
		args         string
		wantRequests int64
	}{
		{repo.List, `{"limit":1000}`, 11},
		{repo.BranchList, `{"repo":"acme/widgets","limit":1000}`, 11},
		{repo.CommitList, `{"repo":"acme/widgets","limit":1000}`, 11},
		{repo.UserSearch, `{"query":"member","limit":100}`, 2},
	} {
		var requests atomic.Int64
		res, err := tc.def.Invoke(context.Background(), pagedEnv(t, hugeList, namedItem, &requests), json.RawMessage(tc.args))
		if err != nil {
			t.Fatalf("%s: %v", tc.def.Info().Name, err)
		}
		if got := requests.Load(); got != tc.wantRequests {
			t.Errorf("%s: %d requests, want %d", tc.def.Info().Name, got, tc.wantRequests)
		}
		if !strings.Contains(res.Text, "Showing the first") {
			t.Errorf("%s: markdown does not say there are more:\n%s", tc.def.Info().Name, res.Text[max(0, len(res.Text)-200):])
		}
	}
}

// user_search filters names client-side, so a query nobody matches must
// stop after a bounded scan and say so rather than read every member.
func TestUserSearchScanIsBounded(t *testing.T) {
	var requests atomic.Int64
	res, err := repo.UserSearch.Invoke(context.Background(), pagedEnv(t, hugeList, namedItem, &requests), json.RawMessage(`{"query":"nobody"}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 20 {
		t.Fatalf("%d requests for a %d-member workspace, want 20 (2000 members)", got, hugeList)
	}
	result := res.Value.(*repo.UserSearchResult)
	if !result.ScanCapped || !strings.Contains(res.Text, "Searched only the first 2000 members") {
		t.Fatalf("scan cap not reported: %+v\n%s", result, res.Text)
	}
}
