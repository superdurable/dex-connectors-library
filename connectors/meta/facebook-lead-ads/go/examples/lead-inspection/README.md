# Facebook Lead Ads lead-inspection Flow

This runnable Flow calls `getLead` through the static `facebook-lead-ads` connection. It stores the lead's ID, form ID, ad ID, and creation time in an Attribute. Form answers are excluded. Optional branches are unwired and fail the Flow when selected.

From the repository root, check the FDG 2.0 graph:

```sh
dexcli visualize connectors/meta/facebook-lead-ads/go/examples/lead-inspection/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/facebook-lead-ads-lead-fdg --open=false
```

Configure `facebook-lead-ads / facebook-lead-ads` in Dex Web using the [connector guide](../../../README.md). From the Go module, run `GOWORK=off go run ./examples/lead-inspection`. Start `FacebookLeadAdsLeadInspection` with a unique Flow ID and `{"leadId":"987654321012345"}`.

For a mock-backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 GOWORK=off go test -tags=integration ./examples/lead-inspection/flow -run TestLeadInspectionUsesDefaultMockWithRealDex` from the Go module. Live Meta authorization remains unverified.
