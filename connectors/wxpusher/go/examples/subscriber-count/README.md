# WxPusher subscriber-count Flow

This runnable Flow uses the static `wxpusher` connection to count subscription records. The `counted` branch stores the aggregate in a Dex Attribute and completes. Optional error branches remain unwired and fail the Flow when selected.

Generate a strict Flow Definition Graph 2.0:

```bash
cd connectors/wxpusher/go
mkdir -p build/subscriber-count
dexcli visualize ./examples/subscriber-count/flow/workflow.go --schema-version 2.0 --json --out ./build/subscriber-count
```

Start a Dex development server, create the `wxpusher` connection in Dex Web, and enter the application token from the provider administration console. Then run:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10071 go run ./examples/subscriber-count
```

In Dex Web **Start Flow**, choose `WxPusherSubscriberCount`, enter a unique Flow ID, and use an empty JSON input:

```json
{}
```

The mock-backed integration test uses a real Dex Worker and Client with a synthetic count and no provider credential. It does not verify live authorization or counts.
