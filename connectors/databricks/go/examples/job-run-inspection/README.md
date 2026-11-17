# Databricks job_run inspection Flow

This worker registers `DatabricksJobRunInspection`, `DatabricksJobListing`, and `DatabricksJobCreation` through the static `databricks` connection. The inspection Flow calls `getJobRun`, persists selected run status, and exposes it through read-only Dex Web RPCs. The listing Flow calls `listJobs` and completes with one page of job summaries. The creation Flow accepts `name`, `notebookPath`, and `existingClusterId`, creates a persistent saved job, and returns its ID. Delete that job in Jobs & Pipelines after a live trial. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/databricks/go`, run:

```bash
dexcli visualize ./examples/job-run-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/databricks-job-run-inspection-fdg
dexcli visualize ./examples/job-run-inspection/flow/job_listing.go --schema-version 2.0 --json --out /tmp/databricks-job-listing-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev -open=false` with `--connector-release-override databricks=<artifact-directory>`. In Dex Web Connections select the `databricks` connection. Follow the [connection guide](../../../README.md) to create a Databricks personal access token and enter it in secret `api_token`. Enter the required `baseUrl` as the HTTPS workspace root. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/databricks/go`:

```bash
GOWORK=off go run ./examples/job-run-inspection
```

Start `DatabricksJobRunInspection` with a unique Flow ID and input `{"runId":"455644833"}`. Replace the sample with a job_run ID returned by Databricks. The Worker connects to Dex Server without an inbound listener.

Start `DatabricksJobListing` with a unique Flow ID and input such as `{"name":"Example job","limit":25}`. Omit `name` to include all visible jobs; use the returned `nextPageToken` as `pageToken` for the next page.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Databricks authorization and provider behavior remain unverified.
