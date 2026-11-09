# Google Tag Manager for Dex Python

This package implements `listAccounts`, `listContainers`, and `listTags` with a renewable Google OAuth connection. It projects bounded account, container, and tag identities. See the [connector guide](../README.md) for Google Cloud setup.

The runnable [accounts Flow](examples/accounts/README.md) uses the static `google-tag-manager` Dex Web connection. From this package run `uv sync --group dev`, `uv run pytest tests`, `uv run ruff check src tests examples`, and `uv run mypy src examples`. Go-recorded provider decisions replay in `tests/test_conformance.py`. Live Google authorization remains unverified.
