# FreshBooks invoice lookup Flow (Go)

This runnable Flow reads one invoice through the static `freshbooks` Dex Web connection. Start with `freshbooks.GetInvoiceInput{AccountID: "KAG77", InvoiceID: 2201278}`. The `Invoice` result is persisted in an Attribute and exposed through summary and display RPCs.

From the connector's `go/` directory run `GOWORK=off go test ./...`. Generate and lint FDG 2.0 with `dexcli visualize examples/invoice/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/freshbooks-fdg --open=false`. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/invoice/flow -run TestInvoiceUsesDefaultMockWithRealDex` to exercise the checked-in mock through a real Worker and Client.

With a FreshBooks OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/invoice` to start the Worker. Live FreshBooks authorization has not been exercised here.
