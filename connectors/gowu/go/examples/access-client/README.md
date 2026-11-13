# Gowu application information Flow

This runnable Flow uses the static `gowu` connection to describe the authorized Gowu app. The `found` branch stores the filtered result in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

Generate a strict Flow Definition Graph 2.0:

```bash
cd connectors/gowu/go
mkdir -p build/access-client
dexcli visualize ./examples/access-client/flow/workflow.go --schema-version 2.0 --json --out ./build/access-client
```

Start a Dex development server, create the `gowu` connection in Dex Web, and enter the App Key and signing secret following the authorization guide. Then run:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10071 go run ./examples/access-client
```

In Dex Web **Start Flow**, choose `GowuAccessClient`, enter a unique Flow ID, and use an empty JSON input:

```json
{}
```

The mock-backed integration test uses a real Dex Worker and Client with synthetic app information and no provider credential. It does not verify live authorization or app data.
