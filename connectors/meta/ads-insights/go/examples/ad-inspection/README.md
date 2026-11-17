# Meta ad inspection Flow

This runnable example includes `MetaAdsInsightsAdInspection` for `getAd`, `MetaAdsInsightsReport` for `getInsights`, and `MetaAdsInsightsAdsListing` for `listAds`, using the static `meta-ads-insights` connection. Each stores typed provider output in a registered Attribute. Optional branches are unwired and fail the Flow when selected.

From the repository root, check the FDG 2.0 graph:

```sh
dexcli visualize connectors/meta/ads-insights/go/examples/ad-inspection/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/meta-ad-inspection-fdg --open=false
```

Configure `meta-ads-insights / meta-ads-insights` in Dex Web using the [connector guide](../../../README.md). From the Go module, run `GOWORK=off go run ./examples/ad-inspection`. Start `MetaAdsInsightsAdInspection` with a unique Flow ID and `{"adId":"123456789012345"}`.

To read performance, start `MetaAdsInsightsReport` with a unique Flow ID and `{"objectId":"987654321012345","datePreset":"last_7d","limit":25,"after":""}`. The object ID may identify an ad account, campaign, ad set, or ad.

To list ads, start `MetaAdsInsightsAdsListing` with a unique Flow ID and `{"accountId":"123456789012345","limit":25,"after":""}`. The account ID is numeric without `act_`; find it in Ads Manager > Account overview or the account selector. `limit` counts ads from 1 to 500, with zero using Meta's default. `after` is a cursor from a previous page, or blank for the first page. The account ID and cursor are not secrets.

For a mock backed real Dex check, start `dexcli dev -open=false -dex-port 9480 -web-port 9481` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9480 GOWORK=off go test -tags=integration ./examples/ad-inspection/flow -run TestAdsListingUsesDefaultMockWithRealDex`. Live Meta authorization remains unverified.
