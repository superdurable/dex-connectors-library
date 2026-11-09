# Databricks TypeScript job_run inspection

This runnable Flow calls `getJobRun` through the static `databricks` connection and persists the selected job_run status. Optional branches are unwired and fail the Flow when selected. Configure `api_token` in Dex Web Connections using the [connection guide](../../../README.md).

From `connectors/databricks/typescript`, run `npm run example:worker`. In another terminal, run `npm run example:start -- <job_run-id>`. The Worker requires the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service.
