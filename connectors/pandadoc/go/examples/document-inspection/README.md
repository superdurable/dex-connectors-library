# PandaDoc document inspection Flow

This runnable Flow calls `getDocument` through the static `pandadoc` connection. It saves the selected document status in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/pandadoc/go`, run:

```bash
dexcli visualize ./examples/document-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/pandadoc-document-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override pandadoc=<artifact-directory>`. In Dex Web Connections select the `pandadoc` connection. Open [PandaDoc Dev Center Configuration](https://app.pandadoc.com/a/#/api-dashboard/configuration), create or reveal a Sandbox or approved Production API key, and enter it in secret `api_key`. Leave `baseUrl` blank for global accounts; enter `https://api.pandadoc.eu/public/v1` for European accounts. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/pandadoc/go`:

```bash
GOWORK=off go run ./examples/document-inspection
```

Start `PandaDocDocumentInspection` with a unique Flow ID and input `{"documentId":"fANp5wW5EwY7AkiTkTGekZ"}`. Replace the sample with an ID returned by PandaDoc when a document is created. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
