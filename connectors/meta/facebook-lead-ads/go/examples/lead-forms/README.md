# Facebook Lead Ads lead-form Flow

This runnable example includes `FacebookLeadAdsLeadForms` for `listLeadForms` and `FacebookLeadAdsLeadListing` for `listLeads` through the static `facebook-lead-ads` connection. Each stores one typed page and continuation cursor in an Attribute. Optional branches are unwired and fail the Flow when selected.

From the repository root, check the FDG 2.0 graph:

```sh
dexcli visualize connectors/meta/facebook-lead-ads/go/examples/lead-forms/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/facebook-lead-ads-forms-fdg --open=false
```

Configure `facebook-lead-ads / facebook-lead-ads` in Dex Web using the [connector guide](../../../README.md). From the Go module, run `GOWORK=off go run ./examples/lead-forms`. Start `FacebookLeadAdsLeadForms` with a unique Flow ID and `{"pageId":"123456789"}`. Pass `nextCursor` back as `after` for another page.

To list lead identities for a form, start `FacebookLeadAdsLeadListing` with a unique Flow ID and `{"formId":"123456789012345","after":""}`. The result excludes form answers.

For a mock-backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 GOWORK=off go test -tags=integration ./examples/lead-forms/flow -run TestLeadFormsUsesDefaultMockWithRealDex` from the Go module. Live Meta authorization remains unverified.
