package httpclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	"github.com/kabilan108/atlas/internal/config"
)

const (
	DefaultConcurrency = 5
	MaxRetries         = 5
	UserAgent          = "atlas-cli/0.1"
	InitialBackoff     = 500 * time.Millisecond
	BackoffMultiplier  = 2
	MaxBackoff         = 30 * time.Second
)

type Client struct {
	httpClient *http.Client
	email      string
	token      string
	userAgent  string
}

func NewClient() (*Client, error) {
	email, token, err := config.GetAtlassianCredentials()
	if err != nil {
		return nil, fmt.Errorf("failed to get credentials: %w", err)
	}

	return &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		email:     email,
		token:     token,
		userAgent: UserAgent,
	}, nil
}

func (c *Client) Do(ctx context.Context, req *http.Request) (*http.Response, error) {
	req = req.WithContext(ctx)

	c.addAuth(req)
	req.Header.Set("User-Agent", c.userAgent)

	var resp *http.Response
	var err error

	for attempt := 0; attempt <= MaxRetries; attempt++ {
		resp, err = c.httpClient.Do(req)
		if err != nil {
			if attempt == MaxRetries {
				return nil, fmt.Errorf("request failed after %d attempts: %w", MaxRetries+1, err)
			}

			backoff := c.calculateBackoff(attempt)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				continue
			}
		}

		// Check if we should retry based on status code
		if !c.shouldRetry(resp.StatusCode) {
			break
		}

		// Handle rate limiting
		if resp.StatusCode == http.StatusTooManyRequests {
			retryAfter := c.getRetryAfter(resp)
			if retryAfter > 0 {
				select {
				case <-ctx.Done():
					resp.Body.Close()
					return nil, ctx.Err()
				case <-time.After(retryAfter):
					resp.Body.Close()
					continue
				}
			}
		}

		// For 5xx errors, use exponential backoff
		if resp.StatusCode >= 500 {
			if attempt == MaxRetries {
				break
			}

			resp.Body.Close()
			backoff := c.calculateBackoff(attempt)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
				continue
			}
		}

		break
	}

	return resp, err
}

func (c *Client) addAuth(req *http.Request) {
	auth := base64.StdEncoding.EncodeToString([]byte(c.email + ":" + c.token))
	req.Header.Set("Authorization", "Basic "+auth)
}

func (c *Client) shouldRetry(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests ||
		statusCode >= 500
}

func (c *Client) getRetryAfter(resp *http.Response) time.Duration {
	retryAfterHeader := resp.Header.Get("Retry-After")
	if retryAfterHeader == "" {
		return 0
	}

	// Try to parse as seconds
	if seconds, err := strconv.Atoi(retryAfterHeader); err == nil {
		return time.Duration(seconds) * time.Second
	}

	// Try to parse as HTTP date
	if t, err := time.Parse(time.RFC1123, retryAfterHeader); err == nil {
		return time.Until(t)
	}

	return 0
}

func (c *Client) calculateBackoff(attempt int) time.Duration {
	backoff := time.Duration(float64(InitialBackoff) * math.Pow(BackoffMultiplier, float64(attempt)))

	// Add jitter (±25%)
	jitter := time.Duration(rand.Float64() * 0.5 * float64(backoff))
	if rand.Intn(2) == 0 {
		backoff += jitter
	} else {
		backoff -= jitter
	}

	// Cap at maximum backoff
	if backoff > MaxBackoff {
		backoff = MaxBackoff
	}

	return backoff
}
