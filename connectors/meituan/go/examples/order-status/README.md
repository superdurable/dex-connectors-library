# Meituan Delivery order-status Flow

This runnable Flow uses the static `meituan` connection to query a delivery order status. The `found` branch stores the typed result in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

Generate a strict Flow Definition Graph 2.0:

```bash
cd connectors/meituan/go
mkdir -p build/order-status
dexcli visualize ./examples/order-status/flow/workflow.go --schema-version 2.0 --json --out ./build/order-status
```

Start a Dex development server, create the `meituan` connection in Dex Web, and enter the AppKey and paired secret following the authorization guide. Then run:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10071 go run ./examples/order-status
```

In Dex Web **Start Flow**, choose `MeituanOrderStatus`, enter a unique Flow ID, and provide a known delivery ID and Meituan order ID:

```json
{"deliveryId":123456,"meituanOrderId":"MT123456789"}
```

The mock-backed integration test uses a real Dex Worker and Client with a synthetic status and no provider credential. It does not verify live authorization or order data.
