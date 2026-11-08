# Google Firestore list Flow

This runnable Go Flow lists documents in one project through the `google-firestore` Dex Web connection. Supply `ListDocumentsInput{ProjectID: "sample-project", CollectionID: "events"}`, optionally with `PageSize` from 1 through 100 and `PageToken` from a prior page. The result is persisted in the Flow Attribute and exposed through Dex Web summary and display RPCs.

From the connector's `go/` directory run `GOWORK=off go test ./...`. Generate and lint FDG 2.0 with `dexcli visualize go/examples/documents/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/google-firestore-fdg --open=false` from the connector directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/documents/flow -run TestDocumentsUsesDefaultMockWithRealDex` to exercise the checked-in mock through a real Worker and Client.

With the Google Firestore OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/documents` to start the Worker. Live Google Firestore authorization has not been exercised here.
