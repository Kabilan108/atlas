import base64
import os
import sys
from urllib.parse import parse_qs, urlparse

import click
import requests
from markdownify import markdownify as _to_md


def _extract_confluence_context(page_url: str) -> tuple[str, str]:
    """Return (rest_api_base, page_id) from a Confluence page URL.

    - rest_api_base looks like: https://<host>[/wiki]/rest/api
    - page_id is extracted from query (?pageId=) or path (/pages/<id>/...)
    """
    parsed = urlparse(page_url)
    if not parsed.scheme or not parsed.netloc:
        raise click.ClickException(f"Invalid URL: {page_url}")

    # Determine base path prefix (include '/wiki' if the URL is under it)
    path = parsed.path or ""
    prefix = ""
    if path.startswith("/wiki/") or path == "/wiki":
        prefix = "/wiki"

    rest_base = f"{parsed.scheme}://{parsed.netloc}{prefix}/rest/api"

    # Try query param first
    qs = parse_qs(parsed.query)
    if "pageId" in qs and qs["pageId"]:
        page_id = qs["pageId"][0]
        return rest_base, page_id

    # Try path pattern: .../pages/<id>/...
    parts = [p for p in path.split("/") if p]
    try:
        idx = parts.index("pages")
        if idx + 1 < len(parts) and parts[idx + 1].isdigit():
            return rest_base, parts[idx + 1]
    except ValueError:
        pass

    # Fallback: pick the last numeric segment
    numeric_segments = [p for p in parts if p.isdigit()]
    if numeric_segments:
        return rest_base, numeric_segments[-1]

    raise click.ClickException("Could not determine Confluence page ID from URL.")


def _build_auth_headers(rest_base: str) -> dict[str, str]:
    token = os.environ.get("ATLASSIAN_TOKEN")
    if not token:
        raise click.ClickException(
            "Missing ATLASSIAN_TOKEN environment variable for authentication."
        )

    # Prefer basic auth if email present; otherwise try bearer
    email = os.environ.get("ATLASSIAN_EMAIL")
    if email:
        userpass = f"{email}:{token}".encode()
        basic = base64.b64encode(userpass).decode()
        return {"Authorization": f"Basic {basic}"}

    # For Atlassian Cloud (*.atlassian.net), Bearer is not supported.
    try:
        host = urlparse(rest_base).netloc
    except Exception:
        host = ""
    if host.endswith("atlassian.net"):
        raise click.ClickException(
            "ATLASSIAN_EMAIL is required for Atlassian Cloud (Basic auth with API token)."
        )

    # Some server/DC deployments support PAT via Bearer
    return {"Authorization": f"Bearer {token}"}


def _fetch_confluence_content(
    rest_base: str, page_id: str, timeout: float = 30.0
) -> tuple[str, str]:
    """Fetch page content using a specified representation.

    Returns (title, content)
    """

    url = f"{rest_base}/content/{page_id}"
    rep = "export_view"
    params = {"expand": f"title,version,body.{rep}"}
    headers = {"Accept": "application/json", "Content-Type": "application/json"}
    headers.update(_build_auth_headers(rest_base))

    resp = requests.get(url, params=params, headers=headers, timeout=timeout)
    if resp.status_code == 401:
        raise click.ClickException(
            "Authentication failed (401). Check ATLASSIAN_EMAIL/TOKEN."
        )
    if resp.status_code == 403:
        raise click.ClickException(
            "Access forbidden (403). Ensure the token has permissions."
        )
    if resp.status_code == 404:
        raise click.ClickException("Page not found (404). Verify the URL/page ID.")
    if not resp.ok:
        raise click.ClickException(
            f"Confluence API error: {resp.status_code} {resp.text[:300]}"
        )

    data = resp.json()
    title = data.get("title") or ""
    content = ((data.get("body") or {}).get(rep) or {}).get("value") or ""
    return title, content


def _format_output(fmt: str, title: str, content: str) -> str:
    fmt = fmt.lower()
    content = _to_md(content, heading_style="ATX")
    if fmt == "xml":
        return f'<document title="{title}">\n{content}\n</document>\n'
    if fmt == "md" or fmt == "markdown":
        return f"```markdown\n# Title: {title}\n\n{content}\n```"
    raise click.ClickException(f"Unsupported format: {fmt}")


@click.group(context_settings={"help_option_names": ["-h", "--help"]})
def cli() -> None:
    """atlask - ask atlassian"""


@cli.command("page")
@click.option(
    "urls",
    "-u",
    "--url",
    multiple=True,
    help="Confluence page URL to fetch (can be passed multiple times)",
)
@click.option(
    "fmt",
    "-f",
    "--format",
    type=click.Choice(["xml", "markdown", "md"], case_sensitive=False),
    default="xml",
    show_default=True,
    help="Output format for page content",
)
@click.option(
    "timeout",
    "-t",
    "--timeout",
    type=float,
    default=30.0,
    show_default=True,
    help="HTTP request timeout in seconds",
)
def page_cmd(urls: tuple[str, ...], fmt: str, timeout: float) -> None:
    """Fetch one or more Confluence pages and print contents."""
    try:
        collected: list[str] = list(urls)
        # If stdin has data, read URLs from there too
        if not sys.stdin.isatty():
            stdin_data = sys.stdin.read()
            for line in stdin_data.splitlines():
                s = line.strip()
                if not s or s.startswith("#"):
                    continue
                collected.append(s)

        if not collected:
            raise click.UsageError(
                "Provide at least one -u/--url or pipe URLs via STDIN."
            )

        for page_url in collected:
            rest_base, page_id = _extract_confluence_context(page_url)
            title, content = _fetch_confluence_content(
                rest_base, page_id, timeout=timeout
            )
            if not content:
                raise click.ClickException(f"Page content is empty for URL: {page_url}")
            output = _format_output(fmt, title=title, content=content)
            sys.stdout.write(output)
            if not output.endswith("\n"):
                sys.stdout.write("\n")
        sys.stdout.flush()
    except click.ClickException:
        raise
    except requests.RequestException as e:
        raise click.ClickException(f"Network error: {e}") from e


if __name__ == "__main__":
    cli()
