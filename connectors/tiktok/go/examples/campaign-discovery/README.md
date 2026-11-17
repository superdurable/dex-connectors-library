# TikTok campaign discovery Flow

These runnable Flows call `listCampaigns`, `listAdgroups`, and `listAds` through the static `tiktok-business` connection. Each saves the selected page in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/tiktok/go`, run:

```bash
dexcli visualize ./examples/campaign-discovery/flow/workflow.go --schema-version 2.0 --json --out /tmp/tiktok-campaign-discovery-fdg
dexcli visualize ./examples/campaign-discovery/flow/adgroups_flow.go --schema-version 2.0 --lint app --json --out /tmp/tiktok-adgroups-fdg
dexcli visualize ./examples/campaign-discovery/flow/ads_flow.go --schema-version 2.0 --lint app --json --out /tmp/tiktok-ads-fdg
dexcli visualize ./examples/campaign-discovery/flow/campaign_status_flow.go --schema-version 2.0 --lint app --json --out /tmp/tiktok-campaign-status-fdg
dexcli visualize ./examples/campaign-discovery/flow/adgroup_status_flow.go --schema-version 2.0 --lint app --json --out /tmp/tiktok-adgroup-status-fdg
dexcli visualize ./examples/campaign-discovery/flow/ad_status_flow.go --schema-version 2.0 --lint app --json --out /tmp/tiktok-ad-status-fdg
dexcli visualize ./examples/campaign-discovery/flow/create_campaign_flow.go --schema-version 2.0 --lint app --json --out /tmp/tiktok-create-campaign-fdg
dexcli visualize ./examples/campaign-discovery/flow/update_campaign_flow.go --schema-version 2.0 --lint app --json --out /tmp/tiktok-update-campaign-fdg
dexcli visualize ./examples/campaign-discovery/flow/create_adgroup_flow.go --schema-version 2.0 --lint app --json --out /tmp/tiktok-create-adgroup-fdg
dexcli visualize ./examples/campaign-discovery/flow/update_adgroup_flow.go --schema-version 2.0 --lint app --json --out /tmp/tiktok-update-adgroup-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override tiktok-business=<artifact-directory>`. In Dex Web Connections select the `tiktok-business` connection. Obtain a Marketing API access token as described in the [connector README](../../../README.md) and enter it in secret `access_token`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/tiktok/go`:

```bash
GOWORK=off go run ./examples/campaign-discovery
```

Start `TikTokCampaignDiscovery` with a unique Flow ID and input `{"advertiserId":"1845123456789012344"}` (replace with your Ads Manager account ID). Use `page` and `pageSize` for pagination. The Worker connects to Dex Server without an inbound listener.

Start `TikTokAdgroupsDiscovery` or `TikTokAdsDiscovery` with the same input shape to read ad groups or ads. Obtain the corresponding Marketing API read access for the advertiser.

Start `TikTokCampaignStatus`, `TikTokAdgroupStatus`, or `TikTokAdStatus` with a unique Flow ID and input such as `{"advertiserId":"1845123456789012344","campaignId":"1845123456789012345","operationStatus":"DISABLE"}`. Replace `campaignId` with `adgroupId` or `adId` for the other Flows. The token needs Marketing API write permission. `DELETE` removes the selected resource.

Start `TikTokCreateCampaign` with `{"advertiserId":"1845123456789012344","name":"Example Campaign","objectiveType":"LEAD_GENERATION","budgetMode":"BUDGET_MODE_TOTAL","budget":50}`. Start `TikTokUpdateCampaign` with `{"advertiserId":"1845123456789012344","campaignId":"1845123456789012345","name":"Updated Campaign"}`. Use a unique Flow ID for each run; confirm an uncertain create in Ads Manager before manually retrying.

Start `TikTokCreateAdgroup` with the required advertiser and campaign IDs, `name`, `billingEvent`, `budget`, `budgetMode`, `optimizationGoal`, `pacing`, `scheduleStartTime`, and `scheduleType`. The Go integration test shows a complete example input. Start `TikTokUpdateAdgroup` with advertiser and ad group IDs plus a replacement `name` or `budget`. Confirm an uncertain create in Ads Manager before manual recovery.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
