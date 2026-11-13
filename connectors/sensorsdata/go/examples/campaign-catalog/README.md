# Sensors Data campaign catalog Flow

This runnable Flow uses static connection `sensorsdata` and reads one channel campaign page. The `listed` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/sensorsdata/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/campaign-catalog/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/sensorsdata-campaign-catalog
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override sensorsdata=<artifact-directory>`. In Dex Web Connections, create `sensorsdata` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/campaign-catalog` and start `SensorsdataCampaignCatalog` with `{}`.

For the local synthetic mock, run `dexcli dev -dex-port 10291 -web-port 10292 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10291 GOWORK=off go test -tags integration ./examples/campaign-catalog/flow -run TestCampaignCatalogUsesDefaultMockWithRealDex -count=1`.
