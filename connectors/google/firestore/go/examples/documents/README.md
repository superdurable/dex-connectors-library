# Google Firestore document Flows

These runnable Go Flows list or delete documents in one project through the `google-firestore` Dex Web connection. For listing, supply `ListDocumentsInput{ProjectID: "sample-project", CollectionID: "events"}`, optionally with `PageSize` from 1 through 100 and `PageToken` from a prior page. For deletion, supply `GetDocumentInput{ProjectID: "sample-project", CollectionID: "events", DocumentID: "sample-event"}`. Each result is persisted in its Flow Attribute and exposed through Dex Web summary and display RPCs.

From the connector's `go/` directory run `GOWORK=off go test ./...`. Generate and lint FDG 2.0 with `dexcli visualize go/examples/documents/flow/delete.go --schema-version 2.0 --lint app --json --out /tmp/google-firestore-fdg --open=false` from the connector directory. With `dexcli dev -open=false -dex-port 9420 -web-port 9421` running, set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9420` and run `GOWORK=off go test -tags=integration ./examples/documents/flow -run '^TestDocumentsUsesDefaultMockWithRealDex$'` to exercise both checked-in mocks through a real Worker and Client.

With the Google Firestore OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/documents` to start the Worker. Live Google Firestore authorization has not been exercised here.
