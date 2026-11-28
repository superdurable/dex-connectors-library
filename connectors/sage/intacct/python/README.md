# Sage Intacct connector for Python

This package implements `getInvoice` and `listInvoices` with the shared [manifest](../connector.yaml) and Go-recorded provider scenarios. See the [connector guide](../README.md) for OAuth setup and retry behavior. The [invoice lookup Flow](examples/invoice_inspection/README.md) and [listing Flow](examples/invoice_listing/README.md) use the static `sage-intacct` Dex Web connection.

Run `uv sync --locked`, `uv run ruff check .`, `uv run mypy --strict src`, and `uv run pytest tests` from this directory. Live Sage Intacct authorization and Dex Web field rendering have not been verified.
