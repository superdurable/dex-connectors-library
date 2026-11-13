# Halo post catalog Flow

This runnable Flow uses static connection `fit2cloud` and reads one article page. The `listed` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/fit2cloud/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/post-catalog/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/fit2cloud-post-catalog
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override fit2cloud=<artifact-directory>`. In Dex Web Connections, create `fit2cloud` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/post-catalog` and start `Fit2cloudPostCatalog` with `{}`.

For the local synthetic mock, run `dexcli dev -dex-port 10311 -web-port 10312 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10311 GOWORK=off go test -tags integration ./examples/post-catalog/flow -run TestPostCatalogUsesDefaultMockWithRealDex -count=1`.
