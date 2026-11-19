# X Post inspection Flow (Go)

These runnable Flows call `getPost` and `searchRecentPosts` through the static `x-twitter` connection. Each stores its typed result in an Attribute and exposes it through read-only Dex Web RPCs. Pass a decimal Post ID from an x.com URL after `/status/` for `XPostInspection`, or start `XRecentPostSearch` with `{"query":"from:ada","maxResults":10}`.

From `go/`, run `GOWORK=off go test ./...`. From the connector directory, generate and lint FDG 2.0 for `go/examples/post-inspection/flow/workflow.go` and `go/examples/post-inspection/flow/recent_search.go` with `dexcli visualize <file> --schema-version 2.0 --lint app --json --out /tmp/x-twitter-go-fdg`. With `dexcli dev -open=false` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/post-inspection/flow -run TestPostInspectionUsesDefaultMockWithRealDex` to exercise the checked-in mock through a real Worker and Client.

Save an X Bearer Token in Dex Web Connections, then run `GOWORK=off go run ./examples/post-inspection` to start the Worker. Live X access has not been exercised here.
