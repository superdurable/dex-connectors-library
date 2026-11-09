# Creatio contact inspection Flow

This runnable Flow calls `getContact` through the static `creatio` connection. It persists the selected contact profile in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/creatio/go`, run:

```bash
dexcli visualize ./examples/contact-inspection/flow/workflow.go --schema-version 2.0 --lint app --open=false --json --out /tmp/creatio-contact-inspection-fdg
```

Run with `--lint app --open=false` for strict FDG 2.0 validation.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override creatio=<artifact-directory>`. In Dex Web Connections, select `creatio` and follow the [connection guide](../../../README.md) to create Creatio client credentials. Enter the required application `baseUrl` and Identity Service `tokenEndpoint`; leave `access_token`, `timeout`, and `maxResponseBytes` blank.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/creatio/go`:

```bash
GOWORK=off go run ./examples/contact-inspection
```

Start `CreatioContactInspection` with a unique Flow ID and input `{"contactId":"90a68db5-cb3a-4f64-bb60-c0a934ee7600"}`. Replace the sample ID with a contact ID you can access. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Real Creatio authorization and retrieval remain unverified.
