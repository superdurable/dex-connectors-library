# SF Express route-history Flow

Install with `uv sync --locked --project connectors/sfexpress/python`. Configure the static `sfexpress` connection in Dex Web Connections with partner ID and secret checkword from [SF Express Open Platform](https://open.sf-express.com/). From this package directory, run `uv run python -m examples.routes.worker` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev -open=false`. Start `SFExpressRoutes` with `{"waybillNumber":"SF1234567890123"}` for a waybill your application owns. Only `found` is wired; optional branches fail the Flow for inspection.

The integration test uses a fake provider with a 503 retry and verifies the route result through a real Dex Worker and Client. Live SF Express authorization remains unverified.
