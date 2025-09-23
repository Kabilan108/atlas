# Atlas CLI

A POSIX-compliant Go CLI tool for fetching Confluence and Bitbucket content and emitting markdown-wrapped output.

## Features

- **Confluence Integration**: Search content and fetch pages by URL or ID
- **Bitbucket Integration**: Search repositories/PRs and fetch PR details with optional diffs
- **Universal Get**: Auto-detect and fetch from Confluence or Bitbucket URLs
- **HTML to Markdown**: Convert HTML content to clean Markdown format
- **Batch Processing**: Process multiple URLs concurrently with configurable worker pools
- **Multiple Output Formats**: Support for fenced code blocks and XML-like output
- **Robust HTTP Client**: Built-in retry logic with exponential backoff for rate limiting
- **POSIX Compliance**: Content to stdout, messages to stderr, stdin support

## Prerequisites

- Go 1.20+
- Atlassian API token and email for authentication

## Installation

```bash
# Build from source
go build -o atlas ./cmd/atlas

# Or run directly
go run ./cmd/atlas --help
```

## Configuration

### Config Files

Atlas loads configuration from the following locations (in order):
1. `~/.config/atlas/config.json`
2. `$XDG_CONFIG_HOME/atlas/config.json`

Example config file:
```json
{
  "atlassian_email": "you@example.com",
  "atlassian_token": "your-api-token",
  "workspace": "my-workspace",
  "confluence_site": "https://company.atlassian.net",
  "space": "DEV"
}
```

### Environment Variables (Optional)

- `ATLASSIAN_EMAIL`: Overrides `atlassian_email` from config
- `ATLASSIAN_TOKEN`: Overrides `atlassian_token` from config

## Usage

### Global Flags

- `--wrap`: Output format (fenced|xmlish, default: fenced)
- `--concurrency`: Number of concurrent requests (default: 5)
- `--verbose`: Enable verbose logging

### Commands

#### Confluence Operations
```bash
# Search Confluence content
atlas confluence search --query "API documentation" [--space DEV] [--limit 10]
atlas confluence search --query "space = DEV and type = page" --cql

# Get Confluence content
atlas confluence get https://company.atlassian.net/wiki/pages/123456
atlas confluence get 123456
echo "123456" | atlas confluence get -
```

#### Bitbucket Operations
```bash
# Search repositories
atlas bitbucket search --type repos --query "api" [--workspace myworkspace]

# Search pull requests
atlas bitbucket search --type prs --query "bug fix" --workspace myworkspace --repo myrepo

# Get pull request details
atlas bitbucket get pr https://bitbucket.org/workspace/repo/pull-requests/42
atlas bitbucket get pr workspace/repo#42 [--diff]
echo "workspace/repo#42" | atlas bitbucket get pr -
```

#### Universal Get
```bash
# Auto-detect URL type and fetch content
atlas get https://company.atlassian.net/wiki/pages/123456
atlas get https://bitbucket.org/workspace/repo/pull-requests/42 [--diff]
echo "https://company.atlassian.net/wiki/pages/123456" | atlas get -
```

#### Version
```bash
atlas version
```

### Output Formats

#### Fenced Format (Default)
```yaml
```yaml
title: Page Title
url: https://example.com/page/123
id: 123
source: confluence
space: DEV
author: John Doe
updated_at: 2023-01-01T12:00:00.000Z
```

# Page Content

This is the converted markdown content.
\```

#### XML-like Format
```bash
atlas get https://example.com/page/123 --wrap=xmlish
```

```xml
<document url="https://example.com/page/123" title="Page Title" id="123" source="confluence">
# Page Content

This is the converted markdown content.
</document>
```

### Examples

```bash
# Get a Confluence page with XML output
atlas confluence get https://company.atlassian.net/wiki/spaces/DEV/pages/123456 --wrap=xmlish

# Search Confluence with CQL
atlas confluence search --query "space = DEV and type = page and title ~ \"API\"" --cql

# Get Bitbucket PR with diff
atlas bitbucket get pr workspace/repo#42 --diff

# Batch process URLs from file
cat urls.txt | atlas get - --concurrency=10 --verbose

# Search and process all results
atlas confluence search --query "documentation" --space DEV | \
  grep -o 'id: [0-9]*' | cut -d' ' -f2 | \
  atlas confluence get - --wrap=xmlish
```

## Development

```bash
# Run tests
go test ./...

# Run with coverage
go test -cover ./...

# Format code
gofmt -w .

# Vet code
go vet ./...

# Run locally
# Ensure your config file has atlassian_email and atlassian_token set
go run ./cmd/atlas --help

# Or override via environment variables for a one-off run
ATLASSIAN_EMAIL=test@example.com ATLASSIAN_TOKEN=fake-token go run ./cmd/atlas --help
```

### Testing

The project includes comprehensive tests with httptest integration:

```bash
# Run all tests
go test ./...

# Run specific package tests
go test ./internal/parse
go test ./internal/confluence
go test ./internal/bitbucket

# Run with verbose output
go test -v ./...
```

### Project Structure

```
atlas/
├── cmd/atlas/              # CLI entry points
│   ├── main.go             # Main entry point
│   ├── root.go             # Root command and global flags
│   ├── confluence.go       # Confluence subcommands
│   ├── bitbucket.go        # Bitbucket subcommands
│   └── get.go              # Universal get command
├── internal/
│   ├── config/             # Configuration management
│   ├── httpclient/         # HTTP client with retry logic
│   ├── parse/              # URL parsing utilities
│   ├── confluence/         # Confluence API client
│   ├── bitbucket/          # Bitbucket API client
│   ├── convert/            # HTML to Markdown conversion
│   ├── output/             # Output formatting
│   └── worker/             # Concurrent processing
├── go.mod
├── go.sum
└── README.md
```

## License

MIT
