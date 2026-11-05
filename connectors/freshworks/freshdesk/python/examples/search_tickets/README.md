# Freshdesk ticket search

Run `dexcli dev -dex-port 37201 -web-port 37202 -open=false`, configure the `freshdesk` connection in Dex Web, then set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:37201` and run `uv run python -m examples.search_tickets.worker` from `python/`. The Flow accepts a `SearchTicketsInput` JSON object with at least one status, priority, tag, or update date and completes with a `SearchTicketsOutput` page.

Use a separate terminal or Dex Web to start `FreshdeskPythonSearchTickets`. See the parent connector README for API-key setup. This example can also run against the generated mock in its integration test.
