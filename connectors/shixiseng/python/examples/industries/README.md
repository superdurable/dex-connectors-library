# Shixiseng public industries Flow

Install with `uv sync --locked --project connectors/shixiseng/python`. Configure the static `shixiseng` connection in Dex Web Connections using merchant APP_ID and APP_SECRET from [Shixiseng merchant access](https://open.shixiseng.com/). From this package directory, run `uv run python -m examples.industries.worker` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev -open=false`. Start `ShixisengIndustries` with `{}`. Only the `found` branch is wired; optional branches fail the Flow for inspection.

The checked-in integration test uses a fake provider with a 503 retry, then verifies the result through a real Dex Worker and Client. Live merchant authorization remains unverified.
