# Google Analytics 4 for Dex Python

This package implements `runReport`, `getProperty`, and `listAudiences` with a renewable Google OAuth connection. It projects bounded report rows, property metadata, and audience identities. See the [connector guide](../README.md) for Google Cloud setup.

The runnable [property Flow](examples/property/README.md) uses the static `google-analytics-4` Dex Web connection. From this package run `uv sync --group dev`, `uv run pytest tests`, `uv run ruff check src tests examples`, and `uv run mypy src examples`. Go-recorded provider decisions replay in `tests/test_conformance.py`. Live Google authorization remains unverified.
