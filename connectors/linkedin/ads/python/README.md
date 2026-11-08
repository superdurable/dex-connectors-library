# LinkedIn Ads connector for Python

This package implements `listAdAccounts` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup. The [runnable account Flow](examples/accounts/README.md) uses the static `linkedin-ads` Dex Web connection and accepts an cursor-based page size and optional page token in its start input.

Run `uv sync --locked`, `uv run ruff check .`, `uv run mypy --strict src`, and `uv run pytest tests` from this directory. Live LinkedIn authorization and Dex Web field rendering have not been verified.
