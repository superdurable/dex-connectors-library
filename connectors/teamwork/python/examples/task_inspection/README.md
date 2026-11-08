# Teamwork Python task inspection

This runnable Flow calls `getTask` through the static `teamwork` connection, persists the selected task status, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_token` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/teamwork/python`, run `uv run python -m examples.task_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `TeamworkTaskInspectionPython` with a unique Flow ID and input `{"taskId":"12345"}`. Replace the sample task ID with one returned by Teamwork.
