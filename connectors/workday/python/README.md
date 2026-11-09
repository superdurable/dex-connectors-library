# Workday connector for Python

This package implements `getWorker` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup and retry behavior. The [runnable worker Flow](examples/worker_inspection/README.md) uses the static `workday` Dex Web connection.

Run `uv sync --locked`, `uv run ruff check .`, `uv run mypy --strict src`, and `uv run pytest tests` from this directory. Live Workday authorization and Dex Web field rendering have not been verified.
