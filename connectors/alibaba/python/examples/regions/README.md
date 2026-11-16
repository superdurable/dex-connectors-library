# Alibaba Cloud ECS regions Flow

Install this package with `uv sync --locked --project connectors/alibaba/python`. Configure the `alibaba` connection in Dex Web Connections with a RAM AccessKey pair from [Alibaba Cloud RAM](https://ram.console.aliyun.com). Run `uv run --project connectors/alibaba/python python -m examples.regions.worker` from the Python package directory with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev`. Start `AlibabaCloudRegions` with `{}`. Only the `fetched` branch is wired; other outcomes fail the Flow for inspection.

The checked-in integration test uses a fake provider and a real Dex Worker/Client. Real RAM authorization and ECS response behavior remain unverified.
