# SMZDM product-search Flow

Install with `uv sync --locked --project connectors/smzdm/python`. Configure the static `smzdm` connection in Dex Web Connections with App Key and App Secret from [SMZDM Open Platform](https://openapi.smzdm.com/). From this package directory, run `uv run python -m examples.products.worker` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev -open=false`. Start `SMZDMProducts` with `{"keyword":"coffee"}`. Only `listed` is wired; optional branches fail the Flow for inspection.

The integration test uses a fake provider with a 503 retry and verifies the first product page through a real Dex Worker and Client. Live platform authorization remains unverified.
