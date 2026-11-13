# LianLian Pay payment-status Flow

This runnable Flow uses the static `lianlianpay` connection to query one authorized merchant payment status. The `found` branch stores the typed result in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

Generate a strict Flow Definition Graph 2.0:

```bash
cd connectors/lianlianpay/go
mkdir -p build/payment-status
dexcli visualize ./examples/payment-status/flow/workflow.go --schema-version 2.0 --json --out ./build/payment-status
```

Start a Dex development server, create the `lianlianpay` connection in Dex Web, and enter the merchant number and paired PEM private key following the authorization guide. Then run:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10071 go run ./examples/payment-status
```

In Dex Web **Start Flow**, choose `LianLianPaymentStatus`, enter a unique Flow ID, and provide a known merchant order number:

```json
{"merchantOrderId":"ORDER123"}
```

The mock-backed integration test uses a real Dex Worker and Client with a synthetic status and no provider credential. It does not verify live authorization or order data.
