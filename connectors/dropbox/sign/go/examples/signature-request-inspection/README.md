# Dropbox Sign signature request inspection Flow

This runnable Flow calls `getSignatureRequest` through the static `dropbox-sign` connection. It saves the selected status in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/dropbox/sign/go`, run:

```bash
dexcli visualize ./examples/signature-request-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/dropbox-sign-signature-request-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override dropbox-sign=<artifact-directory>`. In Dex Web Connections select the `dropbox-sign` connection. Open [Dropbox Sign API Settings](https://app.hellosign.com/home/settings/api) > **API Keys**, create or reveal a key, and enter it in secret `api_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/dropbox/sign/go`:

```bash
GOWORK=off go run ./examples/signature-request-inspection
```

Start `DropboxSignSignatureRequestInspection` with a unique Flow ID and input `{"signatureRequestId":"fa5c8a0b0f492d768749333ad6fcc214c111e967"}`. Replace the sample with an ID returned when creating a signature request. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
