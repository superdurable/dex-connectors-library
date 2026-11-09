# Adobe Acrobat Sign agreement inspection Flow

This runnable Flow calls `getAgreement` through the static `adobe-acrobat-sign` connection. It saves the selected agreement status in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/adobe/acrobat-sign/go`, run:

```bash
dexcli visualize ./examples/agreement-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/adobe-acrobat-sign-agreement-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override adobe-acrobat-sign=<artifact-directory>`. In Dex Web Connections select the `adobe-acrobat-sign` connection. Follow the [connection guide](../../../README.md) to create an integration key with `agreement_read` scope and enter it in secret `integration_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/adobe/acrobat-sign/go`:

```bash
GOWORK=off go run ./examples/agreement-inspection
```

Start `AdobeAcrobatSignAgreementInspection` with a unique Flow ID and input `{"agreementId":"3AAABLblqZNOTREALAGREEMENTID5_BjiH"}`. Replace the synthetic sample with an ID returned by Acrobat Sign. The Worker connects to Dex Server without an inbound listener.

The mock-backed integration test exercises a real Dex Worker and Server. Provider tests use a local HTTP fake. Real Adobe authorization and lookup remain unverified.
