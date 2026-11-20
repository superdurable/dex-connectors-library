# Udesk Python connector

This package provides the `listTicketStatuses` Query for Udesk ticket status metadata. Configure the static `udesk` connection in Dex Web with an account-specific HTTPS origin, administrator email, and API token. See [the runnable Worker and Flow](examples/list_ticket_statuses/README.md) and [Go-recorded scenarios](../../../conformance/connectors/udesk/scenarios.json). Real Udesk authorization has not been verified.

Run `uv run pytest -q` for conformance and `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/list_ticket_statuses/integration_test.py` against a running Dex stack.
