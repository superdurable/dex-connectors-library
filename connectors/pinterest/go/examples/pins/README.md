# Pinterest list Flow

This runnable Go Flow lists one page of Pins through the `pinterest` Dex Web connection. Start with `ListPinsInput{}` for the default page size, or supply a `PageSize` from 1 through 250 and a `Bookmark` returned by the previous page. The result is persisted in the Flow Attribute and exposed through Dex Web summary and display RPCs.

From the connector's `go/` directory run `GOWORK=off go test ./...`. Generate and lint FDG 2.0 with `dexcli visualize go/examples/pins/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/pinterest-fdg --open=false` from the connector directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/pins/flow -run TestPinsUsesDefaultMockWithRealDex` to exercise the checked-in mock through a real Worker and Client.

With the Pinterest OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/pins` to start the Worker. Live Pinterest authorization has not been exercised here.
