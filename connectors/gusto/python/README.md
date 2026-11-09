# Gusto connector for Python

This package implements `getEmployee` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup and retry behavior. The [runnable employee Flow](examples/employee/README.md) uses the static `gusto` Dex Web connection.

Run `uv sync --locked`, `uv run ruff check .`, `uv run mypy --strict src`, and `uv run pytest tests` from this directory. Live Gusto authorization and Dex Web field rendering have not been verified.
