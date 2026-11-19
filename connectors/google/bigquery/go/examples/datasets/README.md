# Google BigQuery list Flow

This runnable Go Worker registers dataset and table listing Flows through the `google-bigquery` Dex Web connection. Supply `ListDatasetsInput{ProjectID: "sample-project"}` for `GoogleBigQueryDatasetsList`. `GoogleBigQueryTablesList` accepts `ListTablesInput`, such as `{"projectId":"sample-project","datasetId":"reporting"}`. Both results are persisted in Flow Attributes and exposed through Dex Web summary and display RPCs.

From the connector's `go/` directory run `GOWORK=off go test ./...`. Generate and lint the table FDG with `dexcli visualize examples/datasets/flow/tables.go --schema-version 2.0 --lint app --json --out /tmp/google-bigquery-tables-fdg`. With `dexcli dev -open=false -dex-port 9410 -web-port 9411` running, set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9410` and run `GOWORK=off go test -tags=integration ./examples/datasets/flow -run '^TestDatasetsUsesDefaultMockWithRealDex$'` to exercise both checked-in mocks through a real Worker and Client.

With the Google BigQuery OAuth connection saved in Dex Web, run `GOWORK=off go run ./examples/datasets` to start the Worker. Live Google BigQuery authorization has not been exercised here.
