# Braze profile inspection Flow

This runnable Flow calls `getUserProfile` with the static `braze` connection. It saves the selected profile fields in an Attribute and exposes them through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/braze/go`, run:

```bash
dexcli visualize ./examples/profile-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/braze-profile-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override braze=<artifact-directory>`. In Dex Web Connections select the `braze` connection. On Braze Dashboard > Settings > APIs and Identifiers > API Keys, create a REST API key with `users.export.ids` permission and copy its secret value to `rest_api_key`. Set `baseUrl` to the account's REST Endpoint from the same page. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/braze/go`:

```bash
GOWORK=off go run ./examples/profile-inspection
```

Start `BrazeProfileInspection` with a unique Flow ID and input `{"externalId":"sample-user-1"}`. Replace the sample ID with a real external user ID from Braze Dashboard > Audience > Search Users or your application's records. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
