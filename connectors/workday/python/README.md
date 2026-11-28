# Workday connector for Python

This package implements `getWorker` and `listWorkers` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup and retry behavior. The [runnable worker Flow](examples/worker_inspection/README.md) uses the static `workday` Dex Web connection.

Run `uv sync --locked`, `uv run ruff check .`, `uv run mypy --strict src`, and `uv run pytest tests` from this directory. Live Workday authorization and Dex Web field rendering have not been verified.

Version 0.2.0 adds worker search and pagination and uses Connector SDK 0.8.0 to enforce the declared OAuth exchange timeout. Applications that bind custom operations must supply both worker queries. Projects that construct a connection from the provider client or Dex Web settings receive both automatically.
