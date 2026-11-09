# Segment Python source inspection

This runnable Flow calls `getSource` through the static `segment` connection, persists the selected Source identity, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_key` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/twilio/segment/python`, run `uv run python -m examples.source_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `SegmentSourceInspectionPython` with a unique Flow ID and input `{"sourceId":"source123"}`. Replace the sample source ID with one returned by Segment.
