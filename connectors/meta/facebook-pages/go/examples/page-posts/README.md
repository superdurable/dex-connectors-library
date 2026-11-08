# Facebook Pages feed Flow

This runnable Flow calls `listPosts` through the static `facebook-pages` connection. It stores one typed page of post IDs, messages, timestamps, permalinks, and a continuation cursor in an Attribute. Optional branches are unwired and fail the Flow when selected.

From the repository root, check the FDG 2.0 graph:

```sh
dexcli visualize connectors/meta/facebook-pages/go/examples/page-posts/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/facebook-pages-posts-fdg --open=false
```

Configure `facebook-pages / facebook-pages` in Dex Web using the [connector guide](../../../README.md). From the Go module, run `GOWORK=off go run ./examples/page-posts`. Start `FacebookPagesPagePosts` with a unique Flow ID and `{"pageId":"123456789"}`. Pass `nextCursor` back as `after` for another page.

For a mock-backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 GOWORK=off go test -tags=integration ./examples/page-posts/flow -run TestPagePostsUsesDefaultMockWithRealDex` from the Go module. Live Meta authorization remains unverified.
