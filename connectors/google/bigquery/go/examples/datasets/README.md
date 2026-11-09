# Google BigQuery list Flow

This runnable Go Flow lists datasets in one project through the `google-bigquery` Dex Web connection. Supply `ListDatasetsInput{ProjectID: "sample-project"}`, optionally with `MaxResults` from 1 through 100 and `PageToken` from a prior page. The result is persisted in the Flow Attribute and exposed through Dex Web summary and display RPCs.

From the connector's `go/` directory run `GOWORK=off go test ./...`. Generate and lint FDG 2.0 with `dexcli visualize go/examples/datasets/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/google-bigquery-fdg --open=false` from the connector directory. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `GOWORK=off go test -tags=integration ./examples/datasets/flow -run TestDatasetsUsesDefaultMockWithRealDex` to exercise the checked-in mock through a real Worker and Client.

With the Google BigQuery OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/datasets` to start the Worker. Live Google BigQuery authorization has not been exercised here.
