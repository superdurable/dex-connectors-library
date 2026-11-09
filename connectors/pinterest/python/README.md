# Pinterest connector for Python

This package implements `listPins` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup and rate-limit behavior. The [runnable pin Flow](examples/pins/README.md) uses the static `pinterest` Dex Web connection.

Run `uv sync --locked`, `uv run ruff check .`, `uv run mypy --strict src`, and `uv run pytest tests` from this directory. Live Pinterest authorization and Dex Web field rendering have not been verified.
