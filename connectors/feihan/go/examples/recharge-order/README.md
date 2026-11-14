# Feihan recharge-order Flow

This runnable Flow uses the static `feihan` connection to query a known merchant order's recharge status. The `found` branch stores the typed result in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

Generate a strict Flow Definition Graph 2.0:

```bash
cd connectors/feihan/go
mkdir -p build/recharge-order
dexcli visualize ./examples/recharge-order/flow/workflow.go --schema-version 2.0 --json --out ./build/recharge-order
```

Start a Dex development server, create the `feihan` connection in Dex Web, and enter the project code, project password, and KeyStr following the authorization guide. Then run:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10071 go run ./examples/recharge-order
```

In Dex Web **Start Flow**, choose `FeihanRechargeOrder`, enter a unique Flow ID, and provide a known merchant order ID:

```json
{"merchantOrderId":"ORDER123"}
```

The mock-backed integration test uses a real Dex Worker and Client with a synthetic status and no provider credential. It does not verify live authorization or order data.
