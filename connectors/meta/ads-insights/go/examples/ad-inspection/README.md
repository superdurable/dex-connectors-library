# Meta ad inspection Flow

This runnable example includes `MetaAdsInsightsAdInspection` for `getAd` and `MetaAdsInsightsReport` for `getInsights`, using the static `meta-ads-insights` connection. Both store typed provider output in registered Attributes. Optional branches are unwired and fail the Flow when selected.

From the repository root, check the FDG 2.0 graph:

```sh
dexcli visualize connectors/meta/ads-insights/go/examples/ad-inspection/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/meta-ad-inspection-fdg --open=false
```

Configure `meta-ads-insights / meta-ads-insights` in Dex Web using the [connector guide](../../../README.md). From the Go module, run `GOWORK=off go run ./examples/ad-inspection`. Start `MetaAdsInsightsAdInspection` with a unique Flow ID and `{"adId":"123456789012345"}`.

To read performance, start `MetaAdsInsightsReport` with a unique Flow ID and `{"objectId":"987654321012345","datePreset":"last_7d","limit":25,"after":""}`. The object ID may identify an ad account, campaign, ad set, or ad.

For a mock backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8841 GOWORK=off go test -tags=integration ./examples/ad-inspection/flow -run TestAdInspectionUsesDefaultMockWithRealDex`. Live Meta authorization remains unverified.
