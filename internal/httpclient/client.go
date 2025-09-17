package httpclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/kabilan108/atlas/internal/config"
)

const (
	// DefaultConcurrency controls the default parallelism used by network workers.
	DefaultConcurrency = 5

	maxRetries    = 5
	baseBackoff   = 500 * time.Millisecond
	backoffFactor = 2
	userAgent     = "atlas-cli/0.1"
)

// Option customizes the HTTP client wrapper.
type Option func(*options)

type options struct {
	httpClient  *http.Client
	credentials config.Credentials
}

// Client wraps http.Client and injects Atlassian specific behaviour.
type Client struct {
	httpClient *http.Client
	authHeader string
}

var (
	jitterSource = rand.New(rand.NewSource(time.Now().UnixNano())) // #nosec G404 -- math/rand is sufficient for backoff jitter
	jitterMu     sync.Mutex
)

// New constructs a Client, sourcing credentials from the environment when not explicitly provided.
func New(opts ...Option) (*Client, error) {
	o := options{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}

	for _, opt := range opts {
		opt(&o)
	}

	if o.credentials.Email == "" || o.credentials.Token == "" {
		creds, err := config.CredentialsFromEnv()
		if err != nil {
			return nil, err
		}
		o.credentials = creds
	}

	if o.httpClient == nil {
		o.httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	authHeader := buildAuthHeader(o.credentials.Email, o.credentials.Token)

	return &Client{
		httpClient: o.httpClient,
		authHeader: authHeader,
	}, nil
}

// WithHTTPClient overrides the default http.Client used by the wrapper.
func WithHTTPClient(hc *http.Client) Option {
	return func(o *options) {
		o.httpClient = hc
	}
}

// WithCredentials injects credentials without looking at environment variables (useful for tests).
func WithCredentials(email, token string) Option {
	return func(o *options) {
		o.credentials = config.Credentials{Email: email, Token: token}
	}
}

// Do executes the request, handling retries, backoff, and required headers.
func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	if ctx == nil {
		return nil, errors.New("context is required")
	}
	if req == nil {
		return nil, errors.New("request is required")
	}

	if err := ensureGetBody(req); err != nil {
		return nil, err
	}

	var lastErr error
	delay := baseBackoff

	for attempt := 0; attempt <= maxRetries; attempt++ {
		attemptReq, err := cloneRequestWithContext(req, ctx)
		if err != nil {
			return nil, err
		}

		decorateRequest(attemptReq, c.authHeader)

		resp, err := c.httpClient.Do(attemptReq)
		if err == nil && !shouldRetry(resp.StatusCode) {
			return resp, nil
		}

		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("request failed with status %d", resp.StatusCode)
			resp.Body.Close()
		}

		if attempt == maxRetries {
			break
		}

		wait := delay
		if err == nil {
			wait = retryAfterDelay(resp, delay)
		} else {
			wait = addJitter(delay)
		}

		if err := sleepWithContext(ctx, wait); err != nil {
			return nil, err
		}
		delay = nextBackoff(wait)
	}

	if lastErr != nil {
		return nil, fmt.Errorf("request failed after %d attempts: %w", maxRetries+1, lastErr)
	}
	return nil, fmt.Errorf("request failed after %d attempts", maxRetries+1)
}

func buildAuthHeader(email, token string) string {
	credentials := fmt.Sprintf("%s:%s", email, token)
	encoded := base64.StdEncoding.EncodeToString([]byte(credentials))
	return "Basic " + encoded
}

func decorateRequest(req *http.Request, authHeader string) {
	req = req.WithContext(req.Context())
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("User-Agent", userAgent)
	if req.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/json")
	}
}

func shouldRetry(status int) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	return status >= http.StatusInternalServerError && status <= http.StatusNetworkAuthenticationRequired
}

func nextBackoff(previous time.Duration) time.Duration {
	next := time.Duration(float64(previous) * backoffFactor)
	if next <= 0 {
		return previous
	}
	return next
}

func retryAfterDelay(resp *http.Response, fallback time.Duration) time.Duration {
	header := resp.Header.Get("Retry-After")
	if header == "" {
		return addJitter(fallback)
	}

	if seconds, err := strconv.Atoi(header); err == nil && seconds > 0 {
		return addJitter(time.Duration(seconds) * time.Second)
	}

	if when, err := http.ParseTime(header); err == nil {
		delay := time.Until(when)
		if delay > 0 {
			return addJitter(delay)
		}
	}

	return addJitter(fallback)
}

func addJitter(delay time.Duration) time.Duration {
	if delay <= 0 {
		return delay
	}

	jitterMu.Lock()
	defer jitterMu.Unlock()

	maxJitter := delay / 2
	if maxJitter <= 0 {
		maxJitter = time.Millisecond
	}

	jitter := time.Duration(jitterSource.Int63n(int64(maxJitter)))
	return delay + jitter
}

func cloneRequestWithContext(req *http.Request, ctx context.Context) (*http.Request, error) {
	clone := req.Clone(ctx)
	if req.Body == nil {
		return clone, nil
	}
	if req.GetBody == nil {
		return nil, errors.New("request body cannot be retried: missing GetBody")
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, fmt.Errorf("clone body: %w", err)
	}
	clone.Body = body
	return clone, nil
}

func ensureGetBody(req *http.Request) error {
	if req.Body == nil || req.GetBody != nil {
		return nil
	}
	buf, err := io.ReadAll(req.Body)
	if err != nil {
		return fmt.Errorf("buffer request body: %w", err)
	}
	if err := req.Body.Close(); err != nil {
		return fmt.Errorf("close request body: %w", err)
	}
	reader := bytes.NewReader(buf)
	req.Body = io.NopCloser(reader)
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf)), nil
	}
	return nil
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
