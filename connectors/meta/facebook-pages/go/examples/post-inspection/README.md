# Facebook Pages post inspection Flow

This runnable Flow calls `getPost` through the static `facebook-pages` connection and stores one typed post identity in an Attribute. Optional branches are unwired and fail the Flow when selected.

From the repository root, check the FDG 2.0 graph:

```sh
dexcli visualize connectors/meta/facebook-pages/go/examples/post-inspection/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/facebook-pages-post-fdg --open=false
```

Configure `facebook-pages / facebook-pages` in Dex Web using the [connector guide](../../../README.md). From the Go module, run `GOWORK=off go run ./examples/post-inspection`. Start `FacebookPagesPostInspection` with a unique Flow ID and `{"postId":"123456789_987654321"}`.

For a mock-backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 GOWORK=off go test -tags=integration ./examples/post-inspection/flow -run TestPostInspectionUsesDefaultMockWithRealDex` from the Go module. Live Meta authorization remains unverified.
