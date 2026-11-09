# Go identity lookup Flow

This runnable Flow calls `getEmailIdentity` through the static `amazon-ses` connection. It stores the selected `EmailIdentityDetails` in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/amazon/ses/go`, run:

```bash
dexcli visualize ./examples/identity-lookup/flow/workflow.go --schema-version 2.0 --json --out /tmp/amazon-ses-identity-lookup-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override amazon-ses=<artifact-directory>`. In Dex Web Connections select the `amazon-ses` connection. Set `region` and secret `access_key_id` and `secret_access_key` as described in the [connector README](../../../README.md). Set `session_token` for temporary credentials. Leave `endpoint`, `timeout`, and `maxResponseBytes` blank for the documented behavior and displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/amazon/ses/go`:

```bash
GOWORK=off go run ./examples/identity-lookup
```

Start `AmazonSESIdentityLookup` with a unique Flow ID and input `{"emailIdentity":"example.com"}`. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
