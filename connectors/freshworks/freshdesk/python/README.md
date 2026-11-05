# Freshdesk Python connector

Install `dex-connector-freshworks-freshdesk==0.1.0`. The package implements
`searchTickets`, `getTicket`, `createTicket`, `updateTicket`, and `addNote` with
operation-specific Step factories. It declares no Triggers.

Bind a `freshdesk` project connection through `new_project_connection`, or use
`Client` with a static credential provider in a local test. The connection needs
the helpdesk `domain` and secret `api_key` described in the [parent README](../README.md).
The optional `maxResponseBytes` setting bounds successful response bodies; blank
uses the manifest default. Credentials are resolved before every request.

The checked-in [search example](examples/search_tickets/README.md) runs through a
Dex Worker. The 48 Go-recorded provider scenarios cover the five operations,
including retries, uncertainty, and heartbeat checkpoints. They are replayed by
`uv run pytest tests/test_conformance.py`. The example integration test uses a
real local Dex stack with the generated mock. Live Freshdesk provider behavior
and Dex Web authorization have not been verified for this package.
