# MeisterTask Python task inspection

This runnable Flow calls `getTask` through the static `meistertask` connection, persists the task summary, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_token` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/meistertask/python`, run `uv run python -m examples.task_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `MeisterTaskTaskInspectionPython` with a unique Flow ID and input `{"taskId":"15"}`. Replace the sample task ID with one returned by MeisterTask.
