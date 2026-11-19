# Dropbox Sign signature request inspection Flow

The same Worker also registers `DropboxSignSignatureRequestList`. Start it with a one-based `page`, `pageSize` from 1 to 100, and optional Dropbox Sign search expression. It returns titles and status flags without signer or document data. `TestSignatureRequestListUsesDefaultMockWithRealDex` exercises the listing against a real local Dex with the connector mock.

The Worker registers `DropboxSignTemplateSend`. Start it with a `templateIds` array from the account's Templates page, a `signers` array with exact template role, name, and email, and `testMode: true` for a nonbinding test. An optional title, subject, and message can be supplied. `TestTemplateSendUsesDefaultMockWithRealDex` exercises this Flow against a real local Dex with the connector mock. Optional rejection and uncertainty branches are unwired.

The Worker also registers `DropboxSignTemplateList`. Start it with `{"page":1,"pageSize":20}` to return accessible template IDs, titles, and signer roles. An optional `query` filters results. `TestTemplateListUsesDefaultMockWithRealDex` exercises it against a real local Dex with the connector mock; optional rejection branches are unwired.

This runnable Flow calls `getSignatureRequest` through the static `dropbox-sign` connection. It saves the selected status in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/dropbox/sign/go`, run:

```bash
dexcli visualize ./examples/signature-request-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/dropbox-sign-signature-request-inspection-fdg
dexcli visualize ./examples/signature-request-inspection/flow/list_flow.go --schema-version 2.0 --lint app --json --out /tmp/dropbox-sign-signature-request-list-fdg
dexcli visualize ./examples/signature-request-inspection/flow/template_send_flow.go --schema-version 2.0 --lint app --json --out /tmp/dropbox-sign-template-send-fdg
dexcli visualize ./examples/signature-request-inspection/flow/template_list_flow.go --schema-version 2.0 --lint app --json --out /tmp/dropbox-sign-template-list-fdg
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
