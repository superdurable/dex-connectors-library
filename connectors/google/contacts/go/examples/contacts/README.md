# Google Contacts list Flow

This runnable Go Flow lists one contact page through the `google-contacts` Dex Web connection. Start with `ListContactsInput{}` for the default page size or supply `pageSize` and a `pageToken` from a prior result. The result is persisted in the Flow attribute and exposed through Dex Web summary and display RPCs.

From the connector's `go/` directory run `GOWORK=off go test ./...`. Generate and lint FDG 2.0 with `dexcli visualize go/examples/contacts/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/google-contacts-fdg --open=false` from the connector directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/contacts/flow -run TestContactsUsesDefaultMockWithRealDex` to exercise the checked-in mock through a real Worker and Client.

With the Google OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/contacts` to start the Worker. Live Google authorization has not been exercised here.
