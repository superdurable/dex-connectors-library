# Go Azure Blob download and listing Flows

This runnable example registers download and listing Flows through the static `microsoft-azure-blob` connection. Each stores its output in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/microsoft/azure-blob/go`, run:

```bash
dexcli visualize ./examples/blob-download/flow/workflow.go --schema-version 2.0 --json --out /tmp/microsoft-azure-blob-blob-download-fdg
dexcli visualize ./examples/blob-download/flow/blob_listing.go --schema-version 2.0 --lint app --json --out /tmp/microsoft-azure-blob-listing-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override microsoft-azure-blob=<artifact-directory>`. In Dex Web Connections select the `microsoft-azure-blob` connection. Set `accountName` and secret `account_key` as described in the [connector README](../../../README.md). Leave `endpoint`, `timeout`, `maxResponseBytes`, and `listPageSize` blank for the documented behavior and displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/microsoft/azure-blob/go`:

```bash
GOWORK=off go run ./examples/blob-download
```

Start `MicrosoftAzureBlobDownload` with a unique Flow ID and input `{"container":"docs","blobName":"example.txt"}`. Start `BlobListingFlow` with `{"container":"docs"}` for a page of names. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
