# vivo get online version Flow

Install with `uv sync --locked --project connectors/vivo/python`. Configure the `vivo` connection in Dex Web Connections using [the provider guide](../../../connector.yaml). From this package directory, run `uv run python -m examples.get_online_version.worker` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev -open=false`. Start `VivoOnlineVersion` with `{"packageName": "com.example.app"}`. Only `found` is wired; optional outcomes fail the Flow for inspection.

The checked-in integration test uses a fake provider and real Dex Worker/Client. Real provider authorization and API availability remain unverified.
