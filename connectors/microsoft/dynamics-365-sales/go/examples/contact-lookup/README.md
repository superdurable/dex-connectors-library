# Go Dynamics 365 Sales contact lookup Flow

This runnable Flow calls `findContact` through the static `microsoft-dynamics-365-sales` connection. It stores the selected `Contact` in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/microsoft/dynamics-365-sales/go`, run:

```bash
dexcli visualize ./examples/contact-lookup/flow/workflow.go --schema-version 2.0 --json --out /tmp/microsoft-dynamics-365-sales-contact-lookup-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override microsoft-dynamics-365-sales=<artifact-directory>`. In Dex Web Connections select the `microsoft-dynamics-365-sales` connection. Set `environmentUrl`, `tenantId`, `clientId`, and secret `client_secret` as described in the [connector README](../../../README.md). Leave `timeout` and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/microsoft/dynamics-365-sales/go`:

```bash
GOWORK=off go run ./examples/contact-lookup
```

Start `MicrosoftDynamics365SalesContactLookup` with a unique Flow ID and input `{"emailAddress":"ada@example.com"}`. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
