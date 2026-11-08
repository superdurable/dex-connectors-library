# Wise Python transfer inspection

This runnable Flow calls `getTransfer` through the static `wise` connection, persists the selected transfer status, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_token` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/wise/python`, run `uv run python -m examples.transfer_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `WiseTransferInspectionPython` with a unique Flow ID and input `{"transferId":"16521632"}`. Replace the sample transfer ID with one returned by Wise.
