# Zendesk Support Python connector

This package implements `searchTickets`, `getTicket`, `createTicket`, and `updateTicket` for Dex Python applications. The manifest's Python version is `v0.1.0`; the Go connector remains on its independent release version.

Use the [read-ticket example](examples/read_ticket/flow.py) to bind the generated `new_get_ticket_step` factory to the static `zendesk-support-desk` connection. Its [Worker](examples/read_ticket/worker.py) loads that connection from Dex project configuration, and the [integration test](examples/read_ticket/integration_test.py) verifies the same Flow on a real Dex Worker and Client against a local provider stand-in. For provider behavior, configure a user-controlled Zendesk connection in Dex Web; no live Zendesk authorization was verified here.

Install this package in the Python application, run `uv sync --group dev`, then `uv run pytest` from this directory. The conformance suite replays Go-recorded requests and outcomes for all four operations.
