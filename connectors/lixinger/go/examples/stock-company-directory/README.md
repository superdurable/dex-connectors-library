# Lixinger stock company directory Flow

This runnable Flow uses static connection `lixinger` to read metadata for the supplied A-share stock codes. The `listed` branch stores the result in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/lixinger/go`, generate and lint strict FDG 2.0:

```sh
dexcli visualize ./examples/stock-company-directory/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/lixinger-stock-company-directory
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override lixinger=<artifact-directory>`. In Dex Web Connections, create `lixinger` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/stock-company-directory` and start `LixingerCompanyCatalog` with `{"stockCodes":["300750"]}`.

For the local synthetic mock, run `dexcli dev -dex-port 10371 -web-port 10372 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10371 GOWORK=off go test -tags integration ./examples/stock-company-directory/flow -run TestCompanyCatalogUsesDefaultMockWithRealDex -count=1`.
