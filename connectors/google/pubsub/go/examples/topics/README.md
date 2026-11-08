# Google PubSub list Flow

This runnable Go Flow lists topics in one project through the `google-pubsub` Dex Web connection. Supply `ListTopicsInput{ProjectID: "sample-project"}`, optionally with `PageSize` from 1 through 100 and `PageToken` from a prior page. The result is persisted in the Flow Attribute and exposed through Dex Web summary and display RPCs.

From the connector's `go/` directory run `GOWORK=off go test ./...`. Generate and lint FDG 2.0 with `dexcli visualize go/examples/topics/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/google-pubsub-fdg --open=false` from the connector directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/topics/flow -run TestTopicsUsesDefaultMockWithRealDex` to exercise the checked-in mock through a real Worker and Client.

With the Google PubSub OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/topics` to start the Worker. Live Google PubSub authorization has not been exercised here.
