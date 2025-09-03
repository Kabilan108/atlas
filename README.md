# atlask

`atlask` — Ask Atlassian. A tiny CLI for fetching Confluence page content and printing it as XML or Markdown.

## Features
- Fetch one or more Confluence pages by URL (flags or stdin).
- Output formats: `xml` or `markdown`.
- Supports Atlassian Cloud (Basic auth with API token) and some Server/DC (Bearer) deployments via env vars.

## Requirements
- Python `>=3.10`.
- Environment variables:
  - `CONFLUENCE_TOKEN`: required. API token or PAT.
  - `CONFLUENCE_EMAIL`: required for `*.atlassian.net` (Atlassian Cloud). Optional for some Server/DC with Bearer PAT.

## Usage

The following environment variables must be set:

- `CONFLUENCE_EMAIL` for `*.atlassian.net` (Atlassian Cloud).
- `CONFLUENCE_TOKEN` for Atlassian Cloud.

#### Installation

```bash
uv tool install git+https://github.com/kabilan108/atlask
```

#### Export a Confluence page to Markdown

```bash
atlask page -u "https://your-domain.atlassian.net/wiki/spaces/SPACE/pages/123456/Page+Title" --format md
```

#### Fetch multiple pages (repeat `-u/--url`) and output XML:

```bash
atlask page -u URL1 -u URL2 --format xml
```

#### Read URLs from stdin (one per line):

```bash
printf '%s\n' URL1 URL2 | atlask page --format md

atlask page -f md <<EOF
URL1
URL2
EOF
```

## License
Apache-2.0. See [`LICENSE`](./LICENSE) for details.
