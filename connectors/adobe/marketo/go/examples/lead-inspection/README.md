# Marketo lead inspection Flow

This runnable Flow calls `getLead` through the static `adobe-marketo` connection. It persists the selected lead profile in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/adobe/marketo/go`, run:

```bash
dexcli visualize ./examples/lead-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/marketo-lead-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override adobe-marketo=<artifact-directory>`. In Dex Web Connections, select `adobe-marketo` and follow the [connection guide](../../../README.md) to configure a Marketo Custom service. Leave `access_token`, `timeout`, and `maxResponseBytes` blank.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/adobe/marketo/go`:

```bash
GOWORK=off go run ./examples/lead-inspection
```

Start `MarketoLeadInspection` with a unique Flow ID and input `{"leadId":318581}`. Replace the sample ID with a lead ID you can access. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Real Marketo authorization and retrieval remain unverified.
