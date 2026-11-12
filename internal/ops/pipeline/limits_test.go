package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kabilan108/atlas/internal/bitbucket"
	"github.com/kabilan108/atlas/internal/ops"
	"github.com/kabilan108/atlas/internal/ops/opstest"
	"github.com/kabilan108/atlas/internal/ops/pipeline"
	"github.com/kabilan108/atlas/internal/ops/pipeline/pipelinetest"
)

const syntheticSecret = "synthetic-secret-5be1f0"

func TestRunRedactsSecuredValuesFromUpstreamErrors(t *testing.T) {
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"type":"error","error":{"message":"Invalid variables","detail":%q}}`, "could not use request "+string(body))
	})
	env := opstest.New("acme").WithBitbucket(t, echo)
	_, err := pipeline.Start.Call(context.Background(), env, pipeline.RunInput{
		RepoRef: ops.RepoRef{Repo: repo}, Branch: "main",
		Variables: []pipeline.Variable{{Key: "TOKEN", Value: syntheticSecret, Secured: true}, {Key: "TARGET", Value: "iam"}},
	})
	if err == nil {
		t.Fatal("want the upstream 400")
	}
	if strings.Contains(err.Error(), syntheticSecret) {
		t.Fatalf("error leaks a secured value: %v", err)
	}
	if !strings.Contains(err.Error(), "Invalid variables") || !strings.Contains(err.Error(), `{"key":"TOKEN","value":"[redacted]","secured":true},{"key":"TARGET","value":"iam"`) {
		t.Fatalf("redaction removed more than the secret: %v", err)
	}
	if ops.KindOf(err) != ops.Invalid {
		t.Fatalf("kind = %v, want invalid", ops.KindOf(err))
	}
	var apiErr *bitbucket.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest || strings.Contains(apiErr.Error(), syntheticSecret) {
		t.Fatalf("the API error must stay reachable and redacted: %#v", apiErr)
	}
}

func TestLogRejectsUnboundedCaps(t *testing.T) {
	env, fixture := newEnv(t)
	for name, in := range map[string]pipeline.LogInput{
		"tail -1":       {TailBytes: -1},
		"max -1":        {MaxBytes: -1},
		"tail over max": {TailBytes: pipeline.MaxTailBytes + 1},
		"call over max": {MaxBytes: pipeline.MaxCallBytes + 1},
	} {
		in.RepoRef, in.Builds = ops.RepoRef{Repo: repo}, []string{"2090"}
		if _, err := pipeline.Log.Call(context.Background(), env, in); ops.KindOf(err) != ops.Invalid {
			t.Errorf("%s: err = %v, want invalid", name, err)
		}
	}
	if n := len(fixture.Requests()); n != 0 {
		t.Fatalf("made %d requests", n)
	}
}

// hugeLog streams a log far larger than any cap and ignores Range, as a
// misbehaving server or storage proxy might.
type hugeLog struct {
	fixture *pipelinetest.Fixture
	written atomic.Int64
}

const hugeLogSize = 256 << 20

func (h *hugeLog) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasSuffix(r.URL.Path, "/steps/{s-backup}/log") {
		h.fixture.ServeHTTP(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	line := []byte(strings.Repeat("x", 1023) + "\n")
	for written := 0; written < hugeLogSize; written += len(line) {
		if _, err := w.Write(line); err != nil {
			return
		}
		h.written.Add(int64(len(line)))
	}
}

func TestLogNeverReadsAHugeLogWhole(t *testing.T) {
	huge := &hugeLog{fixture: pipelinetest.New()}
	env := opstest.New("acme").WithBitbucket(t, huge)
	result, err := pipeline.Log.Call(context.Background(), env, pipeline.LogInput{
		RepoRef: ops.RepoRef{Repo: repo}, Builds: []string{"2090"}, TailBytes: pipeline.MaxTailBytes, MaxBytes: pipeline.MaxCallBytes,
	})
	if written := huge.written.Load(); written >= hugeLogSize/2 {
		t.Fatalf("server wrote %d of %d bytes; the client read the log whole", written, hugeLogSize)
	}
	if err == nil {
		if step := result.Results[0].Steps[0]; step.Bytes > pipeline.MaxTailBytes {
			t.Fatalf("step shows %d bytes", step.Bytes)
		}
	}
}

// slowPolls delays every poll of the started run until the request is
// cancelled or delay passes.
type slowPolls struct {
	fixture *pipelinetest.Fixture
	delay   time.Duration
	polls   atomic.Int32
}

func (s *slowPolls) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	poll := strings.HasSuffix(r.URL.Path, fmt.Sprintf("/pipelines/%d", pipelinetest.Started)) ||
		strings.HasSuffix(r.URL.Path, fmt.Sprintf("/pipelines/{p-%d}", pipelinetest.Started))
	if r.Method == http.MethodGet && poll {
		s.polls.Add(1)
		select {
		case <-r.Context().Done():
			return
		case <-time.After(s.delay):
		}
	}
	s.fixture.ServeHTTP(w, r)
}

func TestRunWaitStopsAtTheDeadline(t *testing.T) {
	for name, interval := range map[string]time.Duration{"default polls": 0, "poll in flight at the deadline": 300 * time.Millisecond} {
		t.Run(name, func(t *testing.T) {
			if interval > 0 {
				pipeline.SetPolls(t, interval, interval)
			}
			slow := &slowPolls{fixture: pipelinetest.New(), delay: 1500 * time.Millisecond}
			env := opstest.New("acme").WithBitbucket(t, slow)
			started := time.Now()
			receipt, err := pipeline.Start.Call(context.Background(), env, pipeline.RunInput{RepoRef: ops.RepoRef{Repo: repo}, Branch: "main", WaitSeconds: 1})
			elapsed := time.Since(started)
			if err != nil {
				t.Fatalf("reaching the deadline is not an error: %v", err)
			}
			if elapsed > 1200*time.Millisecond {
				t.Fatalf("returned after %s for a 1s wait (%d polls)", elapsed, slow.polls.Load())
			}
			if receipt.Finished || receipt.BuildNumber != pipelinetest.Started {
				t.Fatalf("receipt = %+v", receipt)
			}
			mustContain(t, markdown(t, receipt.Markdown), "after waiting 1s; it keeps running")
		})
	}
}
