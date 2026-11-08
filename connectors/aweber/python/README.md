# AWeber connector for Python

This package implements `listAccounts` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup and rate-limit behavior. The [runnable account Flow](examples/accounts/README.md) uses the static `aweber` Dex Web connection.

Run `uv sync --locked`, `uv run ruff check .`, `uv run mypy --strict src`, and `uv run pytest tests` from this directory. Live AWeber authorization and Dex Web field rendering have not been verified.
