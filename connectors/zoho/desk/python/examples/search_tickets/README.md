# Zoho Desk ticket search

Run `dexcli dev -dex-port 40870 -web-port 40871 -open=false`, configure the `zoho-desk-helpdesk` OAuth connection in Dex Web, then set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:40870` and run `uv run python -m examples.search_tickets.worker` from `python/`. The Flow accepts a `SearchTicketsInput` JSON object with at least one filter and completes with a `SearchTicketsOutput` page.

In another terminal, run `uv run python -m examples.search_tickets.main` to start the sample request. The integration test uses the generated mock and a real local Dex Worker and Client. Live Zoho authorization and provider delivery remain unverified.
