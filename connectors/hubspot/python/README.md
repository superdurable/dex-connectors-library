# HubSpot Python connector

This package implements HubSpot CRM search, get, upsert, and update operations for Dex Python Flows. `connector.yaml` defines operation identities, typed Steps, and the private app and OAuth connection contracts. OAuth refresh and shared credential handling are generated from the manifest.

Use `new_project_connection(project, "hubspot-crm")` to bind the connection configured in Dex Web. The runnable Flow in [`examples/lead_qualification`](examples/lead_qualification/) demonstrates the connection and operation-specific Step factories.

Run `uv sync --dev` followed by `uv run pytest` from this directory. The conformance suite replays Go-recorded HubSpot responses without a real account. Live HubSpot authorization and provider behavior require a user-controlled Dex Web connection.
