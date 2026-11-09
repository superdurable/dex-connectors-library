# Keap contact lookup Flow

This runnable Flow reads one Keap contact with `getContact` through the static `keap` connection and persists its profile. Configure secret `personalAccessToken` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/keap/go`, run:

```bash
dexcli visualize ./examples/contact-read/flow/workflow.go --schema-version 2.0 --json --out /tmp/keap-contact-read-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override keap=<artifact-directory>`. In Dex Web Connections select the `keap` connection. Follow the [connection guide](../../../README.md) to enter secret `personalAccessToken`; leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/keap/go`:

```bash
GOWORK=off go run ./examples/contact-read
```

Start `KeapContactRead` with a unique Flow ID and input `{"contactId":"123"}`. Replace the sample ID with the ID of a contact in your Keap CRM. The Worker connects to Dex Server without an inbound listener.

CI runs the fixture-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Keap authorization and lookup remain unverified.
