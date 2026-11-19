# WxPusher get subscriber count Flow

Install with `uv sync --locked --project connectors/wxpusher/python`. Configure the `wxpusher` connection in Dex Web Connections using [the provider guide](../../../connector.yaml). From this package directory, run `uv run python -m examples.subscriber_count.worker` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev -open=false`. Start `WxPusherSubscriberCount` with `{}`. Only `counted` is wired; optional outcomes fail the Flow for inspection.

The checked-in integration test uses a fake provider and real Dex Worker/Client. Real provider authorization and API availability remain unverified.
