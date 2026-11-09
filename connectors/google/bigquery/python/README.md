# BigQuery connector for Python

This package implements `listDatasets` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup. The [runnable dataset Flow](examples/datasets/README.md) uses the static `google-bigquery` Dex Web connection and requires a project ID in its start input.

Run `uv sync --locked`, `uv run ruff check .`, `uv run mypy --strict src`, and `uv run pytest tests` from this directory. Live BigQuery authorization and Dex Web field rendering have not been verified.
