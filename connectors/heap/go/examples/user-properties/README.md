# Heap user properties Flow

This runnable Flow calls `setUserProperties` through the static `heap` connection, stores the acceptance result in a Dex Attribute, and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected. Configure `appId` in Dex Web Connections using the [connection guide](../../../README.md). The Flow input is `{"identity":"member-1","properties":{"Plan":"Pro"}}`; replace the sample identity and property with non-secret values from your application.

## Strict FDG 2.0 generation

From `connectors/heap/go`, run:

```bash
dexcli visualize ./examples/user-properties/flow/workflow.go --schema-version 2.0 --json --out /tmp/heap-user-properties-fdg
```

The generated graph must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override heap=<artifact-directory>`. In Dex Web Connections select the `heap` connection, enter the numeric app ID, and save. With the project's `DEX_PROJECT_*` environment set, run from `connectors/heap/go`:

```bash
GOWORK=off go run ./examples/user-properties
```

Start `HeapUserProperties` with a unique Flow ID and the input above. The Worker connects to Dex Server without an inbound listener.

CI runs mock-backed and fixture-provider real-Dex integration tests, including a 503 retry. Local provider and deterministic Flow tests use `GOWORK=off go test ./...`. Real Heap acceptance remains unverified.
