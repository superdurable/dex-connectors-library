# Basecamp connector for Python

This package implements `listAccounts` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup and local pagination behavior. The [runnable account Flow](examples/accounts/README.md) uses the static `basecamp` Dex Web connection.

Run `uv sync --locked`, `uv run ruff check .`, `uv run mypy --strict src`, and `uv run pytest tests` from this directory. Live Basecamp authorization and Dex Web field rendering have not been verified.
