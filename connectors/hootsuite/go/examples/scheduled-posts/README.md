# Hootsuite scheduled posts Flow

This runnable Flow calls `listScheduledPosts` through the static `hootsuite` connection. It stores the typed result in a registered Attribute. Optional branches are unwired and fail the Flow when selected.

From the repository root, check the FDG 2.0 graph:

```sh
dexcli visualize connectors/hootsuite/go/examples/scheduled-posts/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/hootsuite-scheduled-posts-fdg --open=false
```

Configure `hootsuite / hootsuite` in Dex Web using the [connector guide](../../../README.md). From the Go module, run `GOWORK=off go run ./examples/scheduled-posts`. Start `HootsuiteScheduledPosts` with a unique Flow ID and `{"startTime":"2026-11-15T00:00:00Z","endTime":"2026-11-20T00:00:00Z"}` as input, replacing those dates with a current UTC interval of at most four weeks. Pass `nextCursor` as `cursor` for another page.

For a mock backed real Dex check, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8841 GOWORK=off go test -tags=integration ./examples/scheduled-posts/flow -run TestScheduledPostsUsesDefaultMockWithRealDex`. Live Hootsuite authorization and provider calls require an approved developer app and remain unverified.
