# Quanshi room directory Flow

This runnable Flow uses static connection `quanshi` to read the administrator's room directory. The `listed` branch stores the result in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/quanshi/go`, generate and lint strict FDG 2.0:

```sh
dexcli visualize ./examples/room-directory/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/quanshi-room-directory
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override quanshi=<artifact-directory>`. In Dex Web Connections, create `quanshi` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/room-directory` and start `QuanshiRoomCatalog` with `{}`.

For the local synthetic mock, run `dexcli dev -dex-port 10361 -web-port 10362 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10361 GOWORK=off go test -tags integration ./examples/room-directory/flow -run TestRoomCatalogUsesDefaultMockWithRealDex -count=1`.
