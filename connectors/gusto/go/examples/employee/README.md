# Gusto employee lookup Flow (Go)

This runnable Flow reads one employee through the static `gusto` Dex Web connection. Start with `gusto.GetEmployeeInput{EmployeeID: "90a68db5-cb3a-4f64-bb60-c0a934ee7600"}`. The `Employee` result is persisted in an Attribute and exposed through summary and display RPCs.

From the connector's `go/` directory run `GOWORK=off go test ./...`. Generate and lint FDG 2.0 with `dexcli visualize examples/employee/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/gusto-fdg --open=false`. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/employee/flow -run TestEmployeeUsesDefaultMockWithRealDex` to exercise the checked-in mock through a real Worker and Client.

With a Gusto OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/employee` to start the Worker. Live Gusto authorization has not been exercised here.
