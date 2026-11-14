# EasyAPI company service Flow

This runnable Flow uses the static `easyapi` connection to query one authorized invoicing company's service configuration. The `found` branch stores the filtered result in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

Generate a strict Flow Definition Graph 2.0:

```bash
cd connectors/easyapi/go
mkdir -p build/shop
dexcli visualize ./examples/shop/flow/workflow.go --schema-version 2.0 --json --out ./build/shop
```

Start a Dex development server, create the `easyapi` connection in Dex Web, and enter the service Key and Secret following the authorization guide. Then run:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10071 go run ./examples/shop
```

In Dex Web **Start Flow**, choose `EasyAPIShop`, enter a unique Flow ID, and provide the tax number of a company associated with the key pair:

```json
{"taxNumber":"91320211MA1WML8X6T"}
```

The mock-backed integration test uses a real Dex Worker and Client with a synthetic company status and no provider credential. It does not verify live authorization or company data.
