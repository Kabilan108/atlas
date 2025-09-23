package confluence

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/kabilan108/atlas/internal/convert"
	"github.com/kabilan108/atlas/internal/httpclient"
	"github.com/kabilan108/atlas/internal/output"
)

type Client struct {
	httpClient *httpclient.Client
	baseURL    string
}

type SearchResult struct {
	Results []ContentResult `json:"results"`
	Size    int             `json:"size"`
}

type ContentResult struct {
	ID      string  `json:"id"`
	Type    string  `json:"type"`
	Title   string  `json:"title"`
	Space   Space   `json:"space"`
	Version Version `json:"version"`
	Body    Body    `json:"body"`
	Links   Links   `json:"_links"`
}

type Space struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type Version struct {
	Number int    `json:"number"`
	When   string `json:"when"`
	By     User   `json:"by"`
}

type User struct {
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
}

type Body struct {
	Storage Storage `json:"storage"`
}

type Storage struct {
	Value          string `json:"value"`
	Representation string `json:"representation"`
}

type Links struct {
	WebUI string `json:"webui"`
	Base  string `json:"base"`
}

func NewClient(baseURL string) (*Client, error) {
	httpClient, err := httpclient.NewClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
	}, nil
}

func (c *Client) Search(ctx context.Context, query string, space string, cqlMode bool, limit int) ([]output.Document, error) {
	var cql string
	if cqlMode {
		cql = query
	} else {
		cql = fmt.Sprintf("text ~ \"%s\"", query)
		if space != "" {
			cql += fmt.Sprintf(" and space = \"%s\"", space)
		}
	}

	params := url.Values{}
	params.Set("cql", cql)
	params.Set("expand", "body.storage,space,version")
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	searchURL := fmt.Sprintf("%s/wiki/rest/api/search?%s", c.baseURL, params.Encode())

	req, err := http.NewRequest("GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search failed with status %d", resp.StatusCode)
	}

	var searchResult SearchResult
	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	var documents []output.Document
	for _, result := range searchResult.Results {
		doc, err := c.convertToDocument(result)
		if err != nil {
			output.LogError("Failed to convert result %s: %v", result.ID, err)
			continue
		}
		documents = append(documents, *doc)
	}

	return documents, nil
}

func (c *Client) GetContent(ctx context.Context, contentID string) (*output.Document, error) {
	contentURL := fmt.Sprintf("%s/wiki/rest/api/content/%s?expand=body.storage,space,version", c.baseURL, contentID)

	req, err := http.NewRequest("GET", contentURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("get content request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get content failed with status %d", resp.StatusCode)
	}

	var content ContentResult
	if err := json.NewDecoder(resp.Body).Decode(&content); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return c.convertToDocument(content)
}

func (c *Client) convertToDocument(content ContentResult) (*output.Document, error) {
	markdown, err := convert.HtmlToMarkdown(content.Body.Storage.Value)
	if err != nil {
		return nil, fmt.Errorf("failed to convert HTML to markdown: %w", err)
	}

	var webURL string
	if content.Links.Base != "" && content.Links.WebUI != "" {
		webURL = content.Links.Base + content.Links.WebUI
	}

	return &output.Document{
		Title:     content.Title,
		URL:       webURL,
		ID:        content.ID,
		Source:    "confluence",
		Space:     content.Space.Key,
		Author:    content.Version.By.DisplayName,
		UpdatedAt: content.Version.When,
		Content:   markdown,
	}, nil
}

func (c *Client) BuildCQL(query string, space string) string {
	cql := fmt.Sprintf("text ~ \"%s\"", query)
	if space != "" {
		cql += fmt.Sprintf(" and space = \"%s\"", space)
	}
	return cql
}
