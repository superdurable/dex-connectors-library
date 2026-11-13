# Weiling department catalog Flow

This runnable Flow uses static connection `weiling` and reads the accessible department list. The `listed` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/weiling/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/department-directory/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/weiling-department-directory
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override weiling=<artifact-directory>`. In Dex Web Connections, create `weiling` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/department-directory` and start `WeilingDepartmentCatalog` with `{}`.

For the local synthetic mock, run `dexcli dev -dex-port 10411 -web-port 10412 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10411 GOWORK=off go test -tags integration ./examples/department-directory/flow -run TestDepartmentCatalogUsesDefaultMockWithRealDex -count=1`.
