# Repository Guidelines

## Project Structure & Module Organization
- `atlask.py`: main CLI module (`click` group `cli`, command `page`).
- `pyproject.toml`: package metadata and entry point (`atlask = atlask:cli`).
- `README.md`: usage and install instructions.
- `.envrc.template`: sample environment variables; copy to `.envrc` locally.
- No dedicated `tests/` directory yet.

## Build, Test, and Development Commands
- Run locally without install:
  - `python atlask.py page -u <url> --format md`
- Install editable for development:
  - `pip install -e .` (or `uv pip install -e .`)
- Installed CLI usage:
  - `atlask page -u https://... --format xml|md -t 30`
- Environment setup (direnv):
  - `cp .envrc.template .envrc && direnv allow`

## Coding Style & Naming Conventions
- Python ≥3.10, follow PEP 8, 4‑space indentation.
- Use `snake_case` for functions/variables, `UPPER_SNAKE_CASE` for constants/env keys.
- CLI options: lowercase with hyphens; internal helpers start with `_` (e.g., `_build_auth_headers`).
- Prefer small, single‑purpose functions; raise `click.ClickException` for user errors.

## Testing Guidelines
- Framework: pytest (recommended). Place tests under `tests/`.
- Naming: files `test_*.py`; functions `test_*`.
- Example:
  - `tests/test_url_parse.py` covering `_extract_confluence_context` cases.
- Run: `pytest -q` (add `pytest` to dev deps). Aim for coverage of URL parsing, auth header logic, and format output.

## Commit & Pull Request Guidelines
- Commits: Conventional Commits style (e.g., `feat:`, `fix:`, `docs:`, `refactor:`). Example: `fix: handle 403 with clearer message`.
- PRs: include
  - concise description, motivation, and scope
  - usage examples (commands and expected output)
  - linked issues (e.g., `Closes #12`)

## Security & Configuration Tips
- Required env vars: `ATLASSIAN_TOKEN` and (for `*.atlassian.net`) `ATLASSIAN_EMAIL`.
- Never commit secrets. Keep `.envrc` local; only track `.envrc.template`.
- Network errors and 401/403/404 are surfaced with actionable messages—preserve clarity when modifying error handling.
