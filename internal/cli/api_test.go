package cli

import (
	"strings"
	"testing"

	"github.com/kabilan108/atlas/internal/ops"
)

func TestAPICommandPrintsTheBody(t *testing.T) {
	out, reqs, err := runRepoCmd(t, "api", "/2.0/user")
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 || reqs[0] != "GET /2.0/user" || !strings.Contains(out, `"account_id":"557058:ada"`) {
		t.Fatalf("requests = %v, stdout = %q", reqs, out)
	}
}

func TestAPICommandRefusesNonAPIPaths(t *testing.T) {
	_, reqs, err := runRepoCmd(t, "api", "https://evil.example/2.0/user")
	if ops.KindOf(err) != ops.Invalid || len(reqs) != 0 {
		t.Fatalf("err = %v, requests = %v", err, reqs)
	}
}
