# Facebook Lead Ads lead-form Flow

This runnable Flow calls `listLeadForms` through the static `facebook-lead-ads` connection. It stores one typed page of form IDs, names, statuses, and a continuation cursor in an Attribute. Optional branches are unwired and fail the Flow when selected.

From the repository root, check the FDG 2.0 graph:

```sh
dexcli visualize connectors/meta/facebook-lead-ads/go/examples/lead-forms/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/facebook-lead-ads-forms-fdg --open=false
```

Configure `facebook-lead-ads / facebook-lead-ads` in Dex Web using the [connector guide](../../../README.md). From the Go module, run `GOWORK=off go run ./examples/lead-forms`. Start `FacebookLeadAdsLeadForms` with a unique Flow ID and `{"pageId":"123456789"}`. Pass `nextCursor` back as `after` for another page.

For a mock-backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 GOWORK=off go test -tags=integration ./examples/lead-forms/flow -run TestLeadFormsUsesDefaultMockWithRealDex` from the Go module. Live Meta authorization remains unverified.
