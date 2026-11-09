# Iterable user inspection Flow

This runnable Flow calls `getUser` through the static `iterable` connection. It persists the selected user identity in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/iterable/go`, run:

```bash
dexcli visualize ./examples/user-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/iterable-user-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override iterable=<artifact-directory>`. In Dex Web Connections select the `iterable` connection. Follow the [connection guide](../../../README.md) to create a Server-side API key and enter it in secret `api_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for US defaults; select the correct API root for EU.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/iterable/go`:

```bash
GOWORK=off go run ./examples/user-inspection
```

Start `IterableUserInspection` with a unique Flow ID and input `{"email":"user@example.com"}`. Replace the sample with an email returned by Iterable. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Iterable authorization and retrieval remain unverified.
