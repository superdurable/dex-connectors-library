# Google Ads list Flow

This runnable Go Flow lists customer resource names directly accessible to the connected Google identity through the `google-ads` Dex Web connection. Supply `ListAccessibleCustomersInput{MaxCustomers: 2}` to bound the list retained in Flow state; zero uses 1,000. The result is persisted in the Flow Attribute and exposed through Dex Web summary and display RPCs.

From the connector's `go/` directory run `GOWORK=off go test ./...`. Generate and lint FDG 2.0 with `dexcli visualize go/examples/customers/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/google-ads-fdg --open=false` from the connector directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/customers/flow -run TestCustomersUsesDefaultMockWithRealDex` to exercise the checked-in mock through a real Worker and Client.

With the Google Ads OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/customers` to start the Worker. Live Google Ads authorization has not been exercised here.
