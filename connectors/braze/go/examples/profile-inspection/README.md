# Braze profile inspection Flow

This runnable Flow calls `listCampaigns` and then `getUserProfile` with the static `braze` connection. It can then trigger a campaign and Canvas for the inspected user when their IDs are supplied. It saves the selected profile fields in an Attribute and exposes them through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/braze/go`, run:

```bash
dexcli visualize ./examples/profile-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/braze-profile-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev -open=false -dex-port 9240 -web-port 9241` with `--connector-release-override braze=<artifact-directory>`. In Dex Web Connections select the `braze` connection. On Braze Dashboard > Settings > APIs and Identifiers > API Keys, create a REST API key with `users.export.ids` permission and copy its secret value to `rest_api_key`. Set `baseUrl` to the account's REST Endpoint from the same page. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/braze/go`:

```bash
GOWORK=off go run ./examples/profile-inspection
```

Start `BrazeProfileInspection` with a unique Flow ID and input `{"externalId":"sample-user-1"}`. Replace the sample ID with a real external user ID from Braze Dashboard > Audience > Search Users or your application's records. The Worker connects to Dex Server without an inbound listener.

To trigger a message, add `campaignId` from **Messaging > Campaigns** for an API-triggered campaign or `canvasId` from **Messaging > Canvas** for an API-triggered Canvas. The REST key also needs `campaigns.trigger.send` or `canvas.trigger.send` for the selected send. Blank IDs skip sending. Both IDs send to the same existing `externalId`; the Flow does not broadcast to an audience.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
