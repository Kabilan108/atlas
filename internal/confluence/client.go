package confluence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/kabilan108/atlas/internal/config"
	"github.com/kabilan108/atlas/internal/convert"
)

const (
	baseURLEnv            = "ATLAS_CONFLUENCE_BASE_URL"
	confluenceSourceLabel = "confluence"
)

// Doer matches the subset of http.Client used by this package.
type Doer interface {
	Do(ctx context.Context, req *http.Request) (*http.Response, error)
}

// Client wraps Confluence REST API operations.
type Client struct {
	doer     Doer
	siteBase *url.URL
	apiBase  *url.URL
}

// Page models a Confluence page with Markdown content.
type Page struct {
	ID        string
	Title     string
	SpaceKey  string
	SpaceName string
	WebURL    string
	Markdown  string
	Author    string
	Updated   time.Time
	Version   int
	Source    string
}

// SearchOptions configures a Confluence search query.
type SearchOptions struct {
	Query string
	CQL   bool
	Space string
	Limit int
}

// SearchResult summarises a Confluence page obtained from search.
type SearchResult struct {
	ID        string
	Title     string
	SpaceKey  string
	SpaceName string
	WebURL    string
	Source    string
}

// NewClient constructs a Confluence client.
func NewClient(doer Doer, cfg config.Config) (*Client, error) {
	if doer == nil {
		return nil, errors.New("confluence client requires a Doer")
	}

	rawBase := strings.TrimSpace(os.Getenv(baseURLEnv))
	if rawBase == "" {
		rawBase = strings.TrimSpace(cfg.ConfluenceSite)
	}
	if rawBase == "" {
		return nil, errors.New("confluence base URL not configured")
	}

	siteBase, err := url.Parse(rawBase)
	if err != nil {
		return nil, fmt.Errorf("parse confluence base URL: %w", err)
	}
	if siteBase.Scheme == "" {
		siteBase.Scheme = "https"
	}

	apiBase, err := deriveAPIBase(siteBase)
	if err != nil {
		return nil, err
	}

	return &Client{doer: doer, siteBase: siteBase, apiBase: apiBase}, nil
}

// GetPage fetches and converts a Confluence page into Markdown.
func (c *Client) GetPage(ctx context.Context, id string) (*Page, error) {
	if id == "" {
		return nil, errors.New("page id is required")
	}

	endpoint := c.apiBase.JoinPath("content", id)

	query := endpoint.Query()
	query.Set("expand", "body.storage,space,version")
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	resp, err := c.doer.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, responseError("get page", resp)
	}

	var payload contentResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	markdown, err := convert.HtmlToMarkdown(payload.Body.Storage.Value)
	if err != nil {
		return nil, fmt.Errorf("convert to markdown: %w", err)
	}

	updated, _ := time.Parse("2006-01-02T15:04:05.000-0700", payload.Version.When)

	page := &Page{
		ID:        payload.ID,
		Title:     payload.Title,
		SpaceKey:  payload.Space.Key,
		SpaceName: payload.Space.Name,
		WebURL:    c.resolveWebURL(payload.Links),
		Markdown:  markdown,
		Author:    payload.Version.By.DisplayName,
		Updated:   updated,
		Version:   payload.Version.Number,
		Source:    confluenceSourceLabel,
	}

	return page, nil
}

// SearchPages retrieves matching pages using the Confluence Search API.
func (c *Client) SearchPages(ctx context.Context, opts SearchOptions) ([]SearchResult, error) {
	cql, err := buildCQL(opts)
	if err != nil {
		return nil, err
	}

	endpoint := c.apiBase.JoinPath("search")

	query := endpoint.Query()
	query.Set("cql", cql)
	if opts.Limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}

	resp, err := c.doer.Do(ctx, req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, responseError("search", resp)
	}

	var payload searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	var results []SearchResult
	for _, result := range payload.Results {
		if result.Content.ID == "" {
			continue
		}
		results = append(results, SearchResult{
			ID:        result.Content.ID,
			Title:     result.Content.Title,
			SpaceKey:  result.Content.Space.Key,
			SpaceName: result.Content.Space.Name,
			WebURL:    c.resolveWebURL(result.Content.Links),
			Source:    confluenceSourceLabel,
		})
	}

	return results, nil
}

func buildCQL(opts SearchOptions) (string, error) {
	if strings.TrimSpace(opts.Query) == "" {
		return "", errors.New("query cannot be empty")
	}

	if opts.CQL {
		return opts.Query, nil
	}

	escaped := strings.ReplaceAll(opts.Query, "\"", "\\\"")
	clause := fmt.Sprintf("text ~ \"%s\"", escaped)
	if opts.Space != "" {
		clause = fmt.Sprintf("space = \"%s\" AND %s", opts.Space, clause)
	}
	return clause, nil
}

func deriveAPIBase(site *url.URL) (*url.URL, error) {
	baseCopy := *site
	path := strings.TrimSuffix(baseCopy.Path, "/")

	switch {
	case strings.HasSuffix(path, "/wiki/rest/api"):
		return &baseCopy, nil
	case strings.HasSuffix(path, "/wiki"):
		path = strings.TrimSuffix(path, "/wiki")
	}

	path = strings.TrimSuffix(path, "/")

	var segments []string
	if path != "" {
		segments = append(segments, strings.TrimPrefix(path, "/"))
	}
	segments = append(segments, "wiki", "rest", "api")

	baseCopy.Path = "/" + strings.Join(segments, "/")
	return &baseCopy, nil
}

func (c *Client) resolveWebURL(links linkSet) string {
	raw := strings.TrimSpace(links.WebUI)
	if raw == "" {
		return ""
	}

	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}

	baseStr := strings.TrimSpace(links.Base)
	var base *url.URL
	var err error
	if baseStr != "" {
		base, err = url.Parse(baseStr)
		if err != nil {
			base = nil
		}
	}

	if base == nil {
		base = c.siteBase
	}

	resolved, err := base.Parse(raw)
	if err != nil {
		return raw
	}

	return resolved.String()
}

func responseError(action string, resp *http.Response) error {
	var apiErr apiError
	if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil && apiErr.Message != "" {
		return fmt.Errorf("confluence %s failed: %s (status %d)", action, apiErr.Message, resp.StatusCode)
	}

	return fmt.Errorf("confluence %s failed: status %d", action, resp.StatusCode)
}

type contentResponse struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
	Space struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"space"`
	Version struct {
		Number int    `json:"number"`
		When   string `json:"when"`
		By     struct {
			DisplayName string `json:"displayName"`
		} `json:"by"`
	} `json:"version"`
	Links linkSet `json:"_links"`
}

type linkSet struct {
	WebUI string `json:"webui"`
	Base  string `json:"base"`
}

type searchResponse struct {
	Results []struct {
		Content struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Space struct {
				Key  string `json:"key"`
				Name string `json:"name"`
			} `json:"space"`
			Links linkSet `json:"_links"`
		} `json:"content"`
	} `json:"results"`
}

type apiError struct {
	Message string `json:"message"`
}
