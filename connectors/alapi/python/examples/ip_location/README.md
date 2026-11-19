# ALAPI IP location Flow

Install this package with `uv sync --locked --project connectors/alapi/python`. Configure the `alapi` connection in Dex Web Connections with the ALAPI token from [the provider console](https://www.alapi.cn/). Run `uv run --project connectors/alapi/python python -m examples.ip_location.worker` from the Python package directory with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev`. Start `ALAPIIPLocation` with `{"ipAddress":"8.8.8.8"}`. Only the `found` branch is wired; other outcomes fail the Flow for inspection.

The checked-in integration test uses a fake provider and a real Dex Worker/Client. Real ALAPI authorization and quota consumption remain unverified.
