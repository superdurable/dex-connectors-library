# HubSpot Python connector

This package implements HubSpot CRM search, get, upsert, and update operations for Dex Python Flows. `connector.yaml` defines operation identities, typed Steps, and the private app and OAuth connection contracts. OAuth refresh and shared credential handling are generated from the manifest.

Use `new_project_connection(project, "hubspot-crm")` to bind the connection configured in Dex Web. The runnable Flow in [`examples/read_contact`](examples/read_contact/) demonstrates the connection and operation-specific Step factory.

Run `uv sync --locked`, `uv run pytest`, `uv run ruff check .`, and `uv run mypy src tests examples` from this directory. The package requires published Connector SDK 0.6.0. The conformance suite replays Go-recorded HubSpot responses without a real account. With `dexcli dev` listening on port 44400, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:44400 uv run pytest examples/read_contact/integration_test.py` to verify the Flow through a real Dex Worker and Client. Live HubSpot authorization and provider behavior require a user-controlled Dex Web connection and remain unverified.
