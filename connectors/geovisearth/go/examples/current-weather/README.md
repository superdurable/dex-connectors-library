# GEOVIS Earth current-weather Flow

This runnable Flow uses static connection `geovisearth` to read one weather observation. The `fetched` branch stores the result in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/geovisearth/go`, generate and lint strict FDG 2.0:

```sh
dexcli visualize ./examples/current-weather/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/geovisearth-current-weather
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override geovisearth=<artifact-directory>`. In Dex Web Connections, create `geovisearth` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/current-weather` and start `GeovisearthCurrentWeather` with `{"location":"WTX_CH101300201"}`.

For the local synthetic mock, run `dexcli dev -dex-port 10351 -web-port 10352 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10351 GOWORK=off go test -tags integration ./examples/current-weather/flow -run TestCurrentWeatherUsesDefaultMockWithRealDex -count=1`.
