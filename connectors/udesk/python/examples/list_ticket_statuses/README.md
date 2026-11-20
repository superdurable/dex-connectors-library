# Udesk listTicketStatuses example

Run `uv run python -m examples.list_ticket_statuses.worker` after configuring the static `udesk` connection in Dex Web. Run `dexcli visualize --open=false` on `examples/list_ticket_statuses/flow.py` to inspect the Flow graph. With a local Dex stack and `DEX_FLOW_SERVICE_ADDRESS`, run `uv run pytest examples/list_ticket_statuses/integration_test.py`. The integration test uses synthetic provider responses; live Udesk authorization remains unverified.
