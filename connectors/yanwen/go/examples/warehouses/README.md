# Yanwen warehouse-list Flow

This runnable Flow uses the static `yanwen` connection to list handover warehouses. The `listed` branch stores the typed result in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

Generate a strict Flow Definition Graph 2.0:

```bash
cd connectors/yanwen/go
mkdir -p build/warehouses
dexcli visualize ./examples/warehouses/flow/workflow.go --schema-version 2.0 --json --out ./build/warehouses
```

Start a Dex development server, create the `yanwen` connection in Dex Web, and enter the customer number and API token following the authorization guide. Then run:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10071 go run ./examples/warehouses
```

In Dex Web **Start Flow**, choose `YanwenWarehouseList`, enter a unique Flow ID, and use an empty input:

```json
{}
```

The mock-backed integration test uses a real Dex Worker and Client with a synthetic warehouse and no provider credential. It does not verify live authorization or warehouse data.
