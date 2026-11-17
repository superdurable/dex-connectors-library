# Lucid document discovery Flow

The runnable Flows call `listDocuments` and `getDocument` through the static `lucid` connection. Each saves its result in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/lucid/go`, run:

```bash
dexcli visualize ./examples/document-discovery/flow/workflow.go --schema-version 2.0 --json --out /tmp/lucid-document-discovery-fdg
dexcli visualize ./examples/document-discovery/flow/document_details.go --schema-version 2.0 --lint app --json --out /tmp/lucid-document-details-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override lucid=<artifact-directory>`. In Dex Web Connections select the `lucid` connection. Create an API key with `DocumentReadonly` in [Developer Portal > API Keys](https://lucid.app/developer) and enter it in secret `api_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/lucid/go`:

```bash
GOWORK=off go run ./examples/document-discovery
```

Start `LucidDocumentDiscovery` with a unique Flow ID and input `{"pageToken":""}`. Copy the returned `nextPageToken` into `pageToken` for another page. Start `LucidDocumentDetails` with a returned ID, for example `{"id":"110808fd-4553-4316-bccf-4f25ff59a532"}`. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
