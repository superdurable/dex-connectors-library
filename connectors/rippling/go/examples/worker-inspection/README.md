# Rippling worker inspection Flow

This runnable Flow calls `getWorker` through the static `rippling` connection. It persists the selected worker status in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/rippling/go`, run:

```bash
dexcli visualize ./examples/worker-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/rippling-worker-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override rippling=<artifact-directory>`. In Dex Web Connections select the `rippling` connection. Follow the [connection guide](../../../README.md) to create a Rippling business personal API token and enter it in secret `api_token`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for Production defaults; select the Sandbox API URL for Sandbox.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/rippling/go`:

```bash
GOWORK=off go run ./examples/worker-inspection
```

Start `RipplingWorkerInspection` with a unique Flow ID and input `{"workerId":"65699281fb7bc7c3510f44a6"}`. Replace the sample with a worker ID returned by Rippling. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Rippling authorization and retrieval remain unverified.
