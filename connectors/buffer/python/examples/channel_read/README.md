# Buffer Python channel lookup

This runnable Flow reads one Buffer channel with `getChannel` through the static `buffer` connection and persists its profile. Configure secret `apiKey` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

From `connectors/buffer/python`, run `uv run python -m examples.channel_read.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `BufferChannelReadPython` with a unique Flow ID and input `{"channelId":"channel-1"}`. Replace the sample IDs with Buffer channel ID from your Buffer account.
