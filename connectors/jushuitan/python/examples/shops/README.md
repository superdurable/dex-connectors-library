# Jushuitan shop-list Flow

Install with `uv sync --locked --project connectors/jushuitan/python`. Configure the `jushuitan` connection in Dex Web Connections with App Key, App Secret, and Access Token from [Jushuitan Open Platform](https://openweb.jushuitan.com/user/login). From the Python package directory, run `uv run python -m examples.shops.worker` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev -open=false`. Start `JushuitanShops` with `{"pageIndex":1,"pageSize":10}`. Only `found` is wired; other outcomes fail the Flow for inspection.

The integration test runs this Flow against real Dex with a fake provider. Real Jushuitan authorization and shop retrieval remain unverified.
