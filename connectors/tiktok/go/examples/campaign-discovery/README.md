# TikTok campaign discovery Flow

This runnable Flow calls `listCampaigns` through the static `tiktok-business` connection. It saves the selected page in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/tiktok/go`, run:

```bash
dexcli visualize ./examples/campaign-discovery/flow/workflow.go --schema-version 2.0 --json --out /tmp/tiktok-campaign-discovery-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override tiktok-business=<artifact-directory>`. In Dex Web Connections select the `tiktok-business` connection. Obtain a Marketing API access token as described in the [connector README](../../../README.md) and enter it in secret `access_token`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/tiktok/go`:

```bash
GOWORK=off go run ./examples/campaign-discovery
```

Start `TikTokCampaignDiscovery` with a unique Flow ID and input `{"advertiserId":"1845123456789012344"}` (replace with your Ads Manager account ID). Use `page` and `pageSize` for pagination. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
