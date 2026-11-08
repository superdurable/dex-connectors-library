# Lucid document discovery Flow

This runnable Flow calls `listDocuments` through the static `lucid` connection. It saves the selected page in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/lucid/go`, run:

```bash
dexcli visualize ./examples/document-discovery/flow/workflow.go --schema-version 2.0 --json --out /tmp/lucid-document-discovery-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override lucid=<artifact-directory>`. In Dex Web Connections select the `lucid` connection. Create an API key with `DocumentReadonly` in [Developer Portal > API Keys](https://lucid.app/developer) and enter it in secret `api_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/lucid/go`:

```bash
GOWORK=off go run ./examples/document-discovery
```

Start `LucidDocumentDiscovery` with a unique Flow ID and input `{"pageToken":""}`. Copy the returned `nextPageToken` into `pageToken` for another page. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
