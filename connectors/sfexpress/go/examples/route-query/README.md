# SF Express route query Flow

This runnable Flow uses the static `sfexpress` connection to query one owned SF Express waybill. The `found` branch stores the typed result in a Dex Attribute and completes. Optional error branches are unwired and fail the Flow when selected.

Generate a strict Flow Definition Graph 2.0:

```bash
cd connectors/sfexpress/go
mkdir -p build/route-query
dexcli visualize ./examples/route-query/flow/workflow.go --schema-version 2.0 --json --out ./build/route-query
```

Start a Dex development server, create the `sfexpress` connection in Dex Web, and enter the customer code and checkword following the authorization guide. Then run:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10071 go run ./examples/route-query
```

In Dex Web **Start Flow**, choose `SFExpressRouteQuery`, enter a unique Flow ID, and provide a waybill owned by this application:

```json
{"waybillNumber":"SF1234567890123"}
```

The mock backed integration test uses a real Dex Worker and Client with a synthetic route and no provider credential. It does not verify production authentication or route data.
