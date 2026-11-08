# Databricks connector

The Go, Python, and TypeScript `getJobRun` Query reads a job run ID, current `status.state`, and optional legacy terminal result state. Task details and user metadata stay out of Dex Results.

## Connect

Follow the [Databricks personal access token guide](https://docs.databricks.com/aws/en/dev-tools/auth/pat): in the target workspace, open **Settings > Developer > Access Tokens > Manage > Generate new token**. Select Other APIs and the `jobs` scope when scope selection is required. Save the opaque secret as `api_token` in Dex Web Connections. Workspace administrators can disable PAT creation.

Set required `baseUrl` to the workspace HTTPS root from the signed-in browser address, with no `/api` path. `timeout` is a per-request Go duration and `maxResponseBytes` is a byte limit; blank uses their displayed defaults. Find a run ID under **Jobs & Pipelines > job > Runs** or in a Jobs API response and pass it as a decimal `runId` string. The implementations accept IDs through 9007199254740991 for exact TypeScript JSON decoding. See [Get a single job run](https://docs.databricks.com/api/jobs/v2/get-run).

## Examples and verification

Run the [Go](go/examples/job-run-inspection/README.md), [Python](python/examples/job_run_inspection/README.md), or [TypeScript](typescript/examples/job-run-inspection/README.md) example with the static `databricks` connection. Go-recorded conformance checks requests, branches, retries, and outputs. Real Dex integration tests use fixture providers. Live Databricks authorization and job run retrieval remain unverified.
