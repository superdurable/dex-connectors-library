# ZEGO room-count Flow

Install with `uv sync --locked --project connectors/zego/python`. Configure the `zego` connection in Dex Web Connections with the numeric App ID and Server Secret from the [ZEGO Console](https://console.zegocloud.com/) > your project > Basic Configurations. From the Python package directory, run `uv run python -m examples.room_count.worker` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev -open=false`. Start `ZEGOGetRoomUserCount` with `{"roomId":"room-1"}` or an existing ZIM room ID. Only `found` is wired; other outcomes fail the Flow for inspection.

The integration test runs this Flow against real Dex with a fake provider. Real ZEGO authorization and room count remain unverified.
