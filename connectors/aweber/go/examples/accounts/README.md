# AWeber list Flow

This runnable Go Flow lists one authorized account page through the `aweber` Dex Web connection. Start with `ListAccountsInput{}` for the default page size or supply a zero-based `Offset` and a `Limit` from 1 through 100. The result is persisted in the Flow Attribute and exposed through Dex Web summary and display RPCs.

From the connector's `go/` directory run `GOWORK=off go test ./...`. Generate and lint FDG 2.0 with `dexcli visualize go/examples/accounts/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/aweber-fdg --open=false` from the connector directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/accounts/flow -run TestAccountsUsesDefaultMockWithRealDex` to exercise the checked-in mock through a real Worker and Client.

With the AWeber OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/accounts` to start the Worker. Live AWeber authorization has not been exercised here.
