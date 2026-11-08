# Jotform form-inspection Flow

This runnable Flow calls `listForms` with the static `jotform` connection. Its happy-path form page is saved in an Attribute and shown through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected. No submission answers or API key enter the Flow result.

## Strict FDG 2.0 generation

From `connectors/jotform/go`, run:

```bash
dexcli visualize ./examples/form-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/jotform-form-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override jotform=<artifact-directory>`. In Dex Web Connections select `jotform / jotform`, create a read-only API key at https://www.jotform.com/myaccount/api, and save it in the secret field. Set `baseUrl` for EU or HIPAA accounts; US accounts can leave it blank for the displayed default.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/jotform/go`:

```bash
GOWORK=off go run ./examples/form-inspection
```

Start `JotformFormInspection` with a unique Flow ID and input `{"offset":0,"limit":20}`. The Worker connects to Dex Server without an inbound listener. The provider connection name is fixed in `flow/workflow.go`.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test -race ./...`.
