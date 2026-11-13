# Suxin organization catalog Flow

This runnable Flow uses static connection `suxin` and reads the accessible organization list. The `listed` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/suxin/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/organization-directory/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/suxin-organization-directory
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override suxin=<artifact-directory>`. In Dex Web Connections, create `suxin` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/organization-directory` and start `SuxinOrganizationCatalog` with `{}`.

For the local synthetic mock, run `dexcli dev -dex-port 10381 -web-port 10382 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10381 GOWORK=off go test -tags integration ./examples/organization-directory/flow -run TestOrganizationCatalogUsesDefaultMockWithRealDex -count=1`.
