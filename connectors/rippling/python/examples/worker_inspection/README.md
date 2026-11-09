# Rippling Python worker inspection

This runnable Flow calls `getWorker` through the static `rippling` connection, persists the selected worker status, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_token` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/rippling/python`, run `uv run python -m examples.worker_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `RipplingWorkerInspectionPython` with a unique Flow ID and input `{"workerId":"65699281fb7bc7c3510f44a6"}`. Replace the sample worker ID with one returned by Rippling.
