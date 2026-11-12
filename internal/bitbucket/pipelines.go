package bitbucket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	PipelineTargetRef         = "pipeline_ref_target"
	PipelineTargetCommit      = "pipeline_commit_target"
	PipelineTargetPullRequest = "pipeline_pullrequest_target"
	PipelineSelectorCustom    = "custom"

	defaultPipelineSort  = "-created_on"
	storageClientTimeout = 60 * time.Second
)

type Pipeline struct {
	UUID              string          `json:"uuid"`
	BuildNumber       int             `json:"build_number"`
	State             PipelineState   `json:"state"`
	Target            PipelineTarget  `json:"target"`
	Trigger           PipelineTrigger `json:"trigger"`
	Creator           User            `json:"creator"`
	CreatedOn         time.Time       `json:"created_on"`
	CompletedOn       *time.Time      `json:"completed_on,omitempty"`
	DurationInSeconds int             `json:"duration_in_seconds"`
	BuildSecondsUsed  int             `json:"build_seconds_used"`
}

type PipelineState struct {
	Name   string              `json:"name"`
	Type   string              `json:"type"`
	Result *PipelineStateLabel `json:"result,omitempty"`
	Stage  *PipelineStateLabel `json:"stage,omitempty"`
}

type PipelineStateLabel struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Status returns the most specific state label: the result once completed
// (SUCCESSFUL, FAILED, STOPPED...), else the stage, else the state name.
func (s PipelineState) Status() string {
	if s.Result != nil && s.Result.Name != "" {
		return s.Result.Name
	}
	if s.Stage != nil && s.Stage.Name != "" {
		return s.Stage.Name
	}
	return s.Name
}

type PipelineTarget struct {
	Type        string               `json:"type"`
	RefType     string               `json:"ref_type,omitempty"`
	RefName     string               `json:"ref_name,omitempty"`
	Commit      *Commit              `json:"commit,omitempty"`
	Selector    *PipelineSelector    `json:"selector,omitempty"`
	Source      string               `json:"source,omitempty"`
	Destination string               `json:"destination,omitempty"`
	PullRequest *PipelinePullRequest `json:"pullrequest,omitempty"`
}

func (t PipelineTarget) Branch() string {
	if t.Type == PipelineTargetPullRequest {
		return t.Source
	}
	if t.RefType == "branch" || t.RefType == "named_branch" {
		return t.RefName
	}
	return ""
}

func (t PipelineTarget) CommitHash() string {
	if t.Commit == nil {
		return ""
	}
	return t.Commit.Hash
}

type PipelineSelector struct {
	Type    string `json:"type"`
	Pattern string `json:"pattern,omitempty"`
}

type PipelinePullRequest struct {
	ID int `json:"id"`
}

type PipelineTrigger struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type PipelineStep struct {
	UUID              string        `json:"uuid"`
	Name              string        `json:"name"`
	State             PipelineState `json:"state"`
	StartedOn         *time.Time    `json:"started_on,omitempty"`
	CompletedOn       *time.Time    `json:"completed_on,omitempty"`
	DurationInSeconds int           `json:"duration_in_seconds"`
	RunNumber         int           `json:"run_number,omitempty"`
}

type PipelineListOptions struct {
	Branch          string
	Commit          string
	SelectorPattern string
	SelectorType    string
	// Sort defaults to -created_on (newest first).
	Sort    string
	PageLen int
	// Limit caps the pipelines returned. Zero returns only the first page,
	// because a repository's pipeline history is unbounded.
	Limit int
}

type PipelineVariable struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Secured bool   `json:"secured"`
}

// PipelineRunInput starts a pipeline on Branch (optionally pinned to Commit) or
// on Commit alone. CustomPipeline selects a custom pipeline by name.
type PipelineRunInput struct {
	Branch         string
	Commit         string
	CustomPipeline string
	Variables      []PipelineVariable
}

type pipelineRunRequest struct {
	Target    pipelineRunTarget  `json:"target"`
	Variables []PipelineVariable `json:"variables,omitempty"`
}

type pipelineRunTarget struct {
	Type     string               `json:"type"`
	RefType  string               `json:"ref_type,omitempty"`
	RefName  string               `json:"ref_name,omitempty"`
	Commit   *pipelineCommitInput `json:"commit,omitempty"`
	Selector *PipelineSelector    `json:"selector,omitempty"`
}

type pipelineCommitInput struct {
	Type string `json:"type"`
	Hash string `json:"hash"`
}

type StepLog struct {
	Content      []byte
	ContentRange string
	// TotalSize is the full log size when known, or -1.
	TotalSize int64
	Partial   bool
}

func PipelineWebURL(workspace, repo string, buildNumber int) string {
	return fmt.Sprintf("https://bitbucket.org/%s/%s/pipelines/results/%d", url.PathEscape(workspace), url.PathEscape(repo), buildNumber)
}

func pipelineListPath(workspace, repo string, opts *PipelineListOptions) string {
	values := url.Values{}
	sort := defaultPipelineSort
	if opts != nil {
		if opts.Branch != "" {
			values.Set("target.branch", opts.Branch)
		}
		if opts.Commit != "" {
			values.Set("target.commit.hash", opts.Commit)
		}
		if opts.SelectorPattern != "" {
			values.Set("target.selector.pattern", opts.SelectorPattern)
		}
		if opts.SelectorType != "" {
			values.Set("target.selector.type", opts.SelectorType)
		}
		if opts.Sort != "" {
			sort = opts.Sort
		}
		if pageLen := pageLenFor(opts.Limit, opts.PageLen); pageLen > 0 {
			values.Set("pagelen", strconv.Itoa(pageLen))
		}
	}
	values.Set("sort", sort)
	return withQuery(repoPath(workspace, repo)+"/pipelines/", values)
}

func (c *Client) ListPipelines(ctx context.Context, workspace, repo string, opts *PipelineListOptions) ([]Pipeline, error) {
	path := pipelineListPath(workspace, repo, opts)
	if opts == nil || opts.Limit <= 0 {
		var page PaginatedResponse[Pipeline]
		if err := c.getJSON(ctx, path, &page, "pipelines"); err != nil {
			return nil, err
		}
		return page.Values, nil
	}
	return collectPages[Pipeline](ctx, c, path, opts.Limit, "pipelines")
}

func isBuildNumber(ref string) bool {
	if ref == "" {
		return false
	}
	for _, r := range ref {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func bracedUUID(value string) string {
	trimmed := strings.Trim(strings.TrimSpace(value), "{}")
	return "{" + trimmed + "}"
}

func pipelineRefSegment(ref string) (string, error) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "#")
	if strings.Trim(ref, "{}") == "" {
		return "", fmt.Errorf("pipeline reference cannot be empty")
	}
	if isBuildNumber(ref) {
		return ref, nil
	}
	return url.PathEscape(bracedUUID(ref)), nil
}

// GetPipeline accepts a build number ("42" or "#42") or a pipeline UUID.
func (c *Client) GetPipeline(ctx context.Context, workspace, repo, ref string) (*Pipeline, error) {
	segment, err := pipelineRefSegment(ref)
	if err != nil {
		return nil, err
	}

	var pipeline Pipeline
	if err := c.getJSON(ctx, repoPath(workspace, repo)+"/pipelines/"+segment, &pipeline, "pipeline"); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, NewNotFoundError("pipeline", ref)
		}
		return nil, err
	}
	return &pipeline, nil
}

// resolvePipelineUUID returns the escaped UUID path segment. Step, log, and stop
// endpoints need the UUID, so build numbers are looked up first.
func (c *Client) resolvePipelineUUID(ctx context.Context, workspace, repo, ref string) (string, error) {
	segment, err := pipelineRefSegment(ref)
	if err != nil {
		return "", err
	}
	if !isBuildNumber(segment) {
		return segment, nil
	}

	pipeline, err := c.GetPipeline(ctx, workspace, repo, segment)
	if err != nil {
		return "", err
	}
	if pipeline.UUID == "" {
		return "", fmt.Errorf("pipeline %s has no UUID in Bitbucket's response", ref)
	}
	return url.PathEscape(bracedUUID(pipeline.UUID)), nil
}

func stepSegment(step string) (string, error) {
	if strings.Trim(strings.TrimSpace(step), "{}") == "" {
		return "", fmt.Errorf("step UUID cannot be empty")
	}
	return url.PathEscape(bracedUUID(step)), nil
}

func (c *Client) ListPipelineSteps(ctx context.Context, workspace, repo, pipeline string) ([]PipelineStep, error) {
	uuid, err := c.resolvePipelineUUID(ctx, workspace, repo, pipeline)
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("%s/pipelines/%s/steps?pagelen=%d", repoPath(workspace, repo), uuid, maxPageLen)
	return collectPages[PipelineStep](ctx, c, path, 0, "pipeline steps")
}

func (c *Client) GetPipelineStep(ctx context.Context, workspace, repo, pipeline, step string) (*PipelineStep, error) {
	uuid, err := c.resolvePipelineUUID(ctx, workspace, repo, pipeline)
	if err != nil {
		return nil, err
	}
	stepUUID, err := stepSegment(step)
	if err != nil {
		return nil, err
	}

	var result PipelineStep
	path := fmt.Sprintf("%s/pipelines/%s/steps/%s", repoPath(workspace, repo), uuid, stepUUID)
	if err := c.getJSON(ctx, path, &result, "pipeline step"); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, NewNotFoundError("pipeline step", step)
		}
		return nil, err
	}
	return &result, nil
}

// maxStepLogResponse bounds how much of one log response is read when the
// server ignores the requested range and sends more than the tail.
const maxStepLogResponse = 16 << 20

// GetPipelineStepLog fetches the last tailBytes bytes of a step log, asking
// for only that range and reading at most maxStepLogResponse bytes of the
// response. Bitbucket may redirect to presigned storage; that hop is fetched
// without credentials or Atlas headers.
func (c *Client) GetPipelineStepLog(ctx context.Context, workspace, repo, pipeline, step string, tailBytes int) (*StepLog, error) {
	if tailBytes <= 0 {
		return nil, fmt.Errorf("pipeline log tail must be positive, got %d", tailBytes)
	}
	byteRange := fmt.Sprintf("bytes=-%d", tailBytes)
	uuid, err := c.resolvePipelineUUID(ctx, workspace, repo, pipeline)
	if err != nil {
		return nil, err
	}
	stepUUID, err := stepSegment(step)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("%s/pipelines/%s/steps/%s/log", repoPath(workspace, repo), uuid, stepUUID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.username, c.password)
	req.Header.Set("Accept", "application/octet-stream, text/plain, */*")
	req.Header.Set("Range", byteRange)

	resp, err := c.withoutRedirects().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if isRedirect(resp.StatusCode) {
		location, err := resp.Location()
		if err != nil {
			return nil, fmt.Errorf("pipeline log redirect has no usable Location: %w", err)
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return c.fetchStepLogFromStorage(ctx, location, tailBytes)
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, NewNotFoundError("pipeline step log", step)
	}
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		if err != nil {
			return nil, err
		}
		if err := checkResponseWithMessage(resp, body); err != nil {
			return nil, err
		}
	}
	return readStepLog(resp, tailBytes)
}

// maxErrorBody bounds how much of a failed log response is read for its
// error message.
const maxErrorBody = 64 << 10

// readStepLog keeps the last tailBytes bytes of a log response. A server
// that honours the range sends no more than that; one that ignores it is
// read up to maxStepLogResponse and then refused, since what was read is
// not the tail.
func readStepLog(resp *http.Response, tailBytes int) (*StepLog, error) {
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		return newStepLog(resp, nil, 0), nil
	}
	limited := io.LimitReader(resp.Body, maxStepLogResponse+1)
	var kept []byte
	var read int64
	chunk := make([]byte, 32<<10)
	for {
		n, err := limited.Read(chunk)
		read += int64(n)
		kept = append(kept, chunk[:n]...)
		if len(kept) > 2*tailBytes {
			kept = append(kept[:0], kept[len(kept)-tailBytes:]...)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	if read > maxStepLogResponse {
		return nil, fmt.Errorf("pipeline log response is over %d bytes although only the last %d were asked for; not reading it", maxStepLogResponse, tailBytes)
	}
	if len(kept) > tailBytes {
		kept = kept[len(kept)-tailBytes:]
	}
	return newStepLog(resp, kept, read), nil
}

func (c *Client) withoutRedirects() *http.Client {
	clone := *c.httpClient
	clone.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &clone
}

func isRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	}
	return false
}

// newStorageClient shares nothing with the authenticated client: no transport
// (which may inject headers), no cookie jar, and no credentials.
func newStorageClient() *http.Client {
	return &http.Client{
		Timeout:   storageClientTimeout,
		Transport: http.DefaultTransport.(*http.Transport).Clone(),
	}
}

func (c *Client) fetchStepLogFromStorage(ctx context.Context, location *url.URL, tailBytes int) (*StepLog, error) {
	if location.Scheme != "https" && location.Scheme != "http" {
		return nil, fmt.Errorf("refusing pipeline log redirect to %s URL", location.Scheme)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Range", fmt.Sprintf("bytes=-%d", tailBytes))

	resp, err := newStorageClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pipeline log from storage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return readStepLog(resp, tailBytes)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if err != nil {
		return nil, err
	}

	message := strings.TrimSpace(string(body))
	if len(message) > 512 {
		message = message[:512] + "..."
	}
	return nil, &APIError{
		StatusCode: resp.StatusCode,
		Message:    fmt.Sprintf("log storage returned HTTP %d: %s", resp.StatusCode, message),
		Resource:   "pipeline log",
	}
}

// newStepLog describes a log response whose last bytes are content and
// whose body was read bytes long.
func newStepLog(resp *http.Response, content []byte, read int64) *StepLog {
	log := &StepLog{
		Content:      content,
		ContentRange: resp.Header.Get("Content-Range"),
		Partial:      resp.StatusCode == http.StatusPartialContent,
		TotalSize:    -1,
	}
	if total, ok := contentRangeTotal(log.ContentRange); ok {
		log.TotalSize = total
	} else if resp.StatusCode == http.StatusOK {
		log.TotalSize = read
	}
	return log
}

func contentRangeTotal(header string) (int64, bool) {
	slash := strings.LastIndex(header, "/")
	if slash < 0 {
		return 0, false
	}
	total, err := strconv.ParseInt(strings.TrimSpace(header[slash+1:]), 10, 64)
	if err != nil {
		return 0, false
	}
	return total, true
}

func pipelineRunBody(input PipelineRunInput) (pipelineRunRequest, error) {
	branch := strings.TrimSpace(input.Branch)
	commit := strings.TrimSpace(input.Commit)
	if branch == "" && commit == "" {
		return pipelineRunRequest{}, fmt.Errorf("pipeline run needs a branch or a commit")
	}

	target := pipelineRunTarget{Type: PipelineTargetCommit}
	if branch != "" {
		target = pipelineRunTarget{Type: PipelineTargetRef, RefType: "branch", RefName: branch}
	}
	if commit != "" {
		target.Commit = &pipelineCommitInput{Type: "commit", Hash: commit}
	}
	if custom := strings.TrimSpace(input.CustomPipeline); custom != "" {
		target.Selector = &PipelineSelector{Type: PipelineSelectorCustom, Pattern: custom}
	}
	return pipelineRunRequest{Target: target, Variables: input.Variables}, nil
}

func (c *Client) RunPipeline(ctx context.Context, workspace, repo string, input PipelineRunInput) (*Pipeline, error) {
	body, err := pipelineRunBody(input)
	if err != nil {
		return nil, err
	}

	_, data, err := c.send(ctx, http.MethodPost, repoPath(workspace, repo)+"/pipelines/", body)
	if err != nil {
		return nil, err
	}

	var pipeline Pipeline
	if err := json.Unmarshal(data, &pipeline); err != nil {
		return nil, fmt.Errorf("failed to parse pipeline response: %w", err)
	}
	return &pipeline, nil
}

func (c *Client) StopPipeline(ctx context.Context, workspace, repo, pipeline string) error {
	uuid, err := c.resolvePipelineUUID(ctx, workspace, repo, pipeline)
	if err != nil {
		return err
	}
	_, _, err = c.send(ctx, http.MethodPost, fmt.Sprintf("%s/pipelines/%s/stopPipeline", repoPath(workspace, repo), uuid), nil)
	return err
}
