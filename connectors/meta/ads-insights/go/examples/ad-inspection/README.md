# Meta ad inspection Flow

This runnable Flow calls `getAd` through the static `meta-ads-insights` connection. It stores the typed ad metadata in a registered Attribute. Optional branches are unwired and fail the Flow when selected.

From the repository root, check the FDG 2.0 graph:

```sh
dexcli visualize connectors/meta/ads-insights/go/examples/ad-inspection/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/meta-ad-inspection-fdg --open=false
```

Configure `meta-ads-insights / meta-ads-insights` in Dex Web using the [connector guide](../../../README.md). From the Go module, run `GOWORK=off go run ./examples/ad-inspection`. Start `MetaAdsInsightsAdInspection` with a unique Flow ID and `{"adId":"123456789012345"}`.

For a mock backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8841 GOWORK=off go test -tags=integration ./examples/ad-inspection/flow -run TestAdInspectionUsesDefaultMockWithRealDex`. Live Meta authorization remains unverified.
