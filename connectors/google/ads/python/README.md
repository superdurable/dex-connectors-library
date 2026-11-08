# Ads connector for Python

This package implements `listAccessibleCustomers` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup. The [runnable customer Flow](examples/customers/README.md) uses the static `google-ads` Dex Web connection and accepts an optional local maximum customer count in its start input.

Run `uv sync --locked`, `uv run ruff check .`, `uv run mypy --strict src`, and `uv run pytest tests` from this directory. Live Ads authorization and Dex Web field rendering have not been verified.
