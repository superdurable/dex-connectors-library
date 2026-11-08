# Wrike Python task inspection

This runnable Flow calls `getTask` through the static `wrike` connection, persists the selected task status, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_token` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/wrike/python`, run `uv run python -m examples.task_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `WrikeTaskInspectionPython` with a unique Flow ID and input `{"taskId":"IEAAAABC"}`. Replace the sample task ID with one returned by Wrike.
