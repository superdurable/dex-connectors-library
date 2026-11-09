# Google Search Console for Dex Python

This package implements the `listSites` Query and a renewable OAuth connection. It returns property URLs and permission levels without account secrets. See the [connector guide](../README.md) for Google Cloud app setup.

The runnable [sites Flow](examples/sites/README.md) uses the static `google-search-console` Dex Web connection. From this package run `uv sync --group dev`, `uv run pytest tests`, `uv run ruff check src tests examples`, and `uv run mypy src examples`. Go-recorded provider decisions replay in `tests/test_conformance.py`. Live Google authorization remains unverified.
