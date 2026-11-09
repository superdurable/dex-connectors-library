# Constant Contact Python connector

The Python package implements the Go-verified `getContact` Query. It reads a contact UUID, optional email address, names, and email permission. OAuth authorization requires `contact_data`; the connection refreshes tokens with `offline_access` and the app's Client Secret. See the [connector guide](../README.md) for setup and [contact inspection](examples/contact_inspection/README.md) for a runnable Flow.

Python replays the Go provider recordings in `conformance/connectors/constantcontact/scenarios.json`. The package is an initial `v0.1.0` release candidate and is not published yet.
