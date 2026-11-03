# Gorgias Python connector

This package implements the six Gorgias ticket and customer operations declared in [`connector.yaml`](../connector.yaml) for Dex Python applications: search tickets, get a ticket, find a customer by email, create a ticket, update a ticket, and add an internal note or public email reply.

Use `new_project_connection(project, "gorgias")` to bind a Dex Web connection. The connection resolves the Gorgias email and API key on each provider call. `new_connection` accepts a configured `Client` for an application that supplies its own credential provider. Close an owned client after the Worker stops.

The runnable `examples/triage_issue` Flow exercises ticket search and detail lookup. For provider behavior, supply a connection through Dex Web and run the example against a real Gorgias account. The local integration test uses a provider stand-in and does not establish live provider authorization.

Run `uv sync --group dev` and `uv run pytest` from this directory. The conformance test replays the Go connector's recorded provider scenarios.
