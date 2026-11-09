# Reddit post lookup Flow (Go)

This runnable Flow reads one post through the static `reddit` Dex Web connection. Start with `reddit.GetPostInput{PostID: "t3_abc123"}`. The `Post` result is persisted in the Flow Attribute and exposed through the summary and display RPCs.

From the connector's `go/` directory, run `GOWORK=off go test ./...`. Generate and lint FDG 2.0 with `dexcli visualize examples/post/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/reddit-fdg --open=false`. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/post/flow -run TestPostUsesDefaultMockWithRealDex` to exercise the checked-in mock through a real Worker and Client.

With an approved Reddit OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/post` to start the Worker. Live Reddit authorization has not been exercised here.
