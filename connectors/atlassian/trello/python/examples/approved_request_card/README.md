# Approved request card Flow

This Flow reads one card through `getCard` and returns its safe card view. It uses the static `trello-requests` connection. Only the `found` branch is wired; other definite outcomes fail the Flow.

From the repository root, generate the connector's strict FDG 2.0 setup reference from its checked-in Go Flow. Dex CLI currently accepts Go source for schema 2.0; the Python Worker below runs the same static connection:

```sh
dexcli visualize connectors/atlassian/trello/go/examples/approved-request-card/flow/workflow.go --schema-version 2.0 --json --out /tmp/trello-python-fdg
```

Start Dex CLI on the example ports:

```sh
dexcli dev -dex-port 22100 -web-port 22101 -flow-rendering-dir /tmp/trello-python-fdg
```

In Dex Web Connections, configure `trello / trello-requests` with the API key and token described by `connector.yaml`. The `endpoint` and `maxResponseBytes` defaults need no edits for the public Trello API. The authorization guide in Dex Web links to the Trello app admin page and token authorization URL.

From `connectors/atlassian/trello/python`, run:

```sh
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:22100 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.approved_request_card.main
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:22100 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.approved_request_card.start trello-card-001 6512f0a1c2d3e4f5a6b7c8d9
```

Run the local real-Dex integration test with the same address. It uses a credential-safe fake Trello HTTP service; it does not verify authorization or behavior against Trello's live API.
