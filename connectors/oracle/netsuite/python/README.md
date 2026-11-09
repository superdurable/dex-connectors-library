# Oracle NetSuite connector for Python

This package implements `getCustomer` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup and retry behavior. The [runnable customer Flow](examples/customer_inspection/README.md) uses the static `oracle-netsuite` Dex Web connection.

Run `uv sync --locked`, `uv run ruff check .`, `uv run mypy --strict src`, and `uv run pytest tests` from this directory. Live Oracle NetSuite authorization and Dex Web field rendering have not been verified.
