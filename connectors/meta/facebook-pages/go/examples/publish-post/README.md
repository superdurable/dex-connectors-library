# Facebook Pages publish-post Flow

This runnable example includes `FacebookPagesPublishTextPost` for publishing and `FacebookPagesUpdatePost` for replacing a post message through the static `facebook-pages` connection. Each stores the accepted post ID in an Attribute. Optional provider rejection and uncertainty branches are unwired and fail the Flow; inspect the Page before any manual retry after uncertainty.

From the repository root, check the FDG 2.0 graph:

```bash
dexcli visualize connectors/meta/facebook-pages/go/examples/publish-post/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/facebook-pages-publish-post-fdg --open=false
```

Configure `facebook-pages / facebook-pages` in Dex Web per the [connector README](../../../README.md). From the Go module, run `GOWORK=off go run ./examples/publish-post`. Start `FacebookPagesPublishTextPost` with a unique Flow ID and `{"pageId":"123456789","message":"Hello from Dex"}`. The Page ID comes from Meta Business Suite > Settings > Business assets > Pages.

To replace an existing post's message, start `FacebookPagesUpdatePost` with a unique Flow ID and `{"postId":"123456789_987654321","message":"Updated from Dex"}`. The post ID comes from the Page feed or `listPosts`.

For a mock-backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8981 GOWORK=off go test -tags=integration ./examples/publish-post/flow -run TestPublishTextPostUsesDefaultMockWithRealDex` from the Go module. Live Meta authorization remains unverified.
