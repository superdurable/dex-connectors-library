# Databricks job_run inspection Flow

This runnable Flow calls `getJobRun` through the static `databricks` connection. It persists the selected job_run status in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/databricks/go`, run:

```bash
dexcli visualize ./examples/job-run-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/databricks-job-run-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override databricks=<artifact-directory>`. In Dex Web Connections select the `databricks` connection. Follow the [connection guide](../../../README.md) to create a Databricks business personal API token and enter it in secret `api_token`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for Production defaults; select the Sandbox API URL for Sandbox.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/databricks/go`:

```bash
GOWORK=off go run ./examples/job-run-inspection
```

Start `DatabricksJobRunInspection` with a unique Flow ID and input `{"runId":"455644833"}`. Replace the sample with a job_run ID returned by Databricks. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Databricks authorization and retrieval remain unverified.
