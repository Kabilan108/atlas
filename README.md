# Atlas CLI

Atlas is a Go-based CLI that fetches Atlassian Confluence and Bitbucket content, then emits consistent, markdown-friendly output. It supports authenticated calls with retry/backoff, optional XML-like wrapping, and parallel fetching for batch workflows.

## Quickstart

```bash
go build -o atlas ./cmd/atlas
./atlas version
```

### Configuration & Credentials

Atlas looks for configuration in the following locations (first match wins):

1. `$XDG_CONFIG_HOME/atlas/config.json`
2. `~/.config/atlas/config.json`
3. `./atlas.json`

Example config file:

```json
{
  "workspace": "my-workspace",
  "confluence_site": "https://example.atlassian.net",
  "space": "ENG"
}
```

Credentials are read from environment variables and are mandatory for real API calls:

- `ATLASSIAN_EMAIL`
- `ATLASSIAN_TOKEN`

For testing you can override service endpoints with:

- `ATLAS_CONFLUENCE_BASE_URL`
- `ATLAS_BITBUCKET_BASE_URL`

## Usage Examples

```bash
# Show command help
go run ./cmd/atlas --help

# Fetch a Confluence page by URL
ATLASSIAN_EMAIL=... ATLASSIAN_TOKEN=... \
go run ./cmd/atlas confluence get https://example.atlassian.net/wiki/spaces/ENG/pages/12345

# Batch fetch Confluence pages from stdin as XML
printf "https://example.atlassian.net/wiki/spaces/ENG/pages/1\n2\n" | \
  ATLASSIAN_EMAIL=... ATLASSIAN_TOKEN=... \
  go run ./cmd/atlas get - --wrap=xmlish

# Search Bitbucket repositories by name
ATLASSIAN_EMAIL=... ATLASSIAN_TOKEN=... \
go run ./cmd/atlas bitbucket search --type repos --workspace my-workspace --query web

# Fetch a Bitbucket pull request with diff
ATLASSIAN_EMAIL=... ATLASSIAN_TOKEN=... \
go run ./cmd/atlas bitbucket get pr my-workspace/repo#42 --diff
```

## Flags & Global Options

- `--wrap {fenced|xmlish}` (default `fenced`) controls stdout formatting.
- `--concurrency N` bounds parallel fetch operations (default `5`).
- `--verbose` enables stderr logging.

## Development

Run formatting, vetting, and tests:

```bash
go test ./...
```

The project requires Go 1.20+.

## Testing Strategy

Unit tests use `httptest` replacements with custom transports, plus environment overrides, to avoid calling real Atlassian services. Retry/backoff logic, parsing helpers, and output formatters are exercised individually so commands can rely on the shared primitives.
