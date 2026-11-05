# Read a HubSpot contact with Dex Python

This Flow reads one contact using the static `hubspot-crm` connection. Only the `found` branch is wired; a missing or rejected record fails the Flow.

Generate the Go example's strict FDG 2.0 reference from `connectors/hubspot`:

```sh
mkdir -p build
cd go
dexcli visualize ./examples/lead-qualification/flow/workflow.go --schema-version 2.0 --json --out ../build/lead-qualification
```

Start `dexcli dev -flow-rendering-dir ./build/lead-qualification` using free ports. Configure `hubspot / hubspot-crm` in Dex Web Connections, and set `DEX_CONNECTOR_CONFIG_FILE` to the resulting owner-only `connections.json`. A private app token or HubSpot OAuth connection can be used. The generated project connection handles OAuth refresh outside Flow state.

From `connectors/hubspot/python`, run:

```sh
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:42701 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json uv run python -m examples.read_contact.main
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:42701 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json uv run python -m examples.read_contact.start hubspot-contact-001 examples/read_contact/sample-input.json
```

The first command stays running as a Worker; run the second in another terminal with a new Flow ID each time. `sample-input.json` contains a contact object type, numeric record ID, and the property names to return. The integration test uses a fake provider through a real Dex Worker and Client; a real HubSpot account is needed to verify live authorization and CRM behavior.
