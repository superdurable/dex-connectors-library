# Amplitude user search Flow

This runnable Flow calls `searchUsers` through the static `amplitude` connection. It persists the matched user IDs in an Attribute and exposes them through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/amplitude/go`, run:

```bash
dexcli visualize ./examples/user-search/flow/workflow.go --schema-version 2.0 --json --out /tmp/amplitude-user-search-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override amplitude=<artifact-directory>`. In Dex Web Connections select the `amplitude` connection. Follow the [connection guide](../../../README.md) to enter secret `apiKey` and `secretKey`; leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/amplitude/go`:

```bash
GOWORK=off go run ./examples/user-search
```

Start `AmplitudeUserSearch` with a unique Flow ID and input `{"user":"example-user"}`. Replace the sample with a user ID or prefix from Amplitude User Lookup. The Worker connects to Dex Server without an inbound listener.

CI runs the fixture-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Amplitude authorization and user search remain unverified.
