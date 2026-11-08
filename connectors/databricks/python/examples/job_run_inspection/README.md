# Databricks Python job_run inspection

This runnable Flow calls `getJobRun` through the static `databricks` connection, persists the selected job_run status, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_token` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/databricks/python`, run `uv run python -m examples.job_run_inspection.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `DatabricksJobRunInspectionPython` with a unique Flow ID and input `{"runId":"455644833"}`. Replace the sample job_run ID with one returned by Databricks.
