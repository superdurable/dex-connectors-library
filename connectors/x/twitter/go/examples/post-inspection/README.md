# X Post inspection Flow (Go)

This runnable Flow calls `getPost` through the static `x-twitter` connection. It stores the Post ID and text in an Attribute and exposes them through read-only Dex Web RPCs. Pass a decimal Post ID from an x.com URL after `/status/`.

From `go/`, run `GOWORK=off go test ./...`. From the connector directory, generate and lint FDG 2.0 with `dexcli visualize go/examples/post-inspection/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/x-twitter-go-fdg --open=false`. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/post-inspection/flow -run TestPostInspectionUsesDefaultMockWithRealDex` to exercise the checked-in mock through a real Worker and Client.

Save an X Bearer Token in Dex Web Connections, then run `GOWORK=off go run ./examples/post-inspection` to start the Worker. Live X access has not been exercised here.
