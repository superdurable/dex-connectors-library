# FastGPT dataset details Flow

This runnable Flow uses static connection `fastgpt` and reads metadata for one knowledge-base ID. The `found` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/fastgpt/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/dataset-details/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/fastgpt-dataset-details
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override fastgpt=<artifact-directory>`. In Dex Web Connections, create `fastgpt` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/dataset-details` and start `FastGPTDatasetDetails` with `{"id":"YOUR_DATASET_ID"}`.

For the local synthetic mock, run `dexcli dev -dex-port 10251 -web-port 10252 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10251 GOWORK=off go test -tags integration ./examples/dataset-details/flow -run TestDatasetDetailsUsesDefaultMockWithRealDex -count=1`.
