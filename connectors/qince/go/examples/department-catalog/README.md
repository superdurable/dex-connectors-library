# Qince department catalog Flow

This runnable Flow uses static connection `qince` and reads the organization department catalog. The `listed` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/qince/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/department-catalog/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/qince-department-catalog
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override qince=<artifact-directory>`. In Dex Web Connections, create `qince` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/department-catalog` and start `QinceDepartmentCatalog` with `{}`.

For the local synthetic mock, run `dexcli dev -dex-port 10281 -web-port 10282 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10281 GOWORK=off go test -tags integration ./examples/department-catalog/flow -run TestDepartmentCatalogUsesDefaultMockWithRealDex -count=1`.
