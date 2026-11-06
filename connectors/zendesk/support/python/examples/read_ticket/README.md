# Read one Zendesk Support ticket

This Flow reads a ticket, its requester, and its latest comments through the static `zendesk-support-desk` connection. Configure the account subdomain, agent email, and API token in Dex Web **Connections** using the [Zendesk setup guide](../../../README.md#zendesk-setup).

From `connectors/zendesk/support/python`, generate the strict FDG 2.0 graph with the latest stable dexcli:

```bash
mkdir -p build
uv run dexcli visualize ./examples/read_ticket/flow.py --schema-version 2.0 --json --out ./build/read-ticket
```

Start `dexcli dev` with `-flow-rendering-dir ./build`, set `DEX_FLOW_SERVICE_ADDRESS` to that stack's Dex gRPC address, and run `uv run python -m examples.read_ticket.worker`. Start Flow type `ZendeskSupportReadTicketPython` from Dex Web with [sample-input.json](sample-input.json). The result and `zendesk-support-read-ticket-details` Attribute hold the ticket and comments.

Run `uv run pytest examples/read_ticket/integration_test.py` with `DEX_FLOW_SERVICE_ADDRESS` set to a running Dex stack to verify the same Flow with a real Worker, retry, and persistence against a local provider stand-in. This does not verify live Zendesk behavior.
