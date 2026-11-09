# Recurly account inspection Flow

This runnable Flow calls `getAccount` through the static `recurly` connection. It persists the selected account identity in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/recurly/go`, run:

```bash
dexcli visualize ./examples/account-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/recurly-account-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override recurly=<artifact-directory>`. In Dex Web Connections select the `recurly` connection. Follow the [connection guide](../../../README.md) to create a Private API Key and enter it in secret `api_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/recurly/go`:

```bash
GOWORK=off go run ./examples/account-inspection
```

Start `RecurlyAccountInspection` with a unique Flow ID and input `{"accountId":"e28zov4fw0v2"}`. Replace the sample with an account ID returned by Recurly. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Recurly authorization and retrieval remain unverified.
