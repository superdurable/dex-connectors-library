# Caiyun Weather current weather Flow

This runnable Flow uses the static `caiyun` connection to read one coordinate's realtime weather. The `fetched` branch stores the result in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/caiyun/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/current-weather/flow/workflow.go --schema-version 2.0 --json --out ./build/current-weather
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override caiyun=<artifact-directory>`. In Dex Web **Connections**, create the `caiyun` connection. At [Caiyun's platform](https://platform.caiyunapp.com), open **访问控制 > API 凭证管理**, create or select an App Key and paired App Secret with weather v2.6 realtime access, and enter both as secrets. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/current-weather
```

In Dex Web **Start Flow**, choose `CaiyunCurrentWeather` and enter a unique Flow ID and this input:

```json
{"longitude":116.3176,"latitude":39.9760}
```

Longitude precedes latitude; the Beijing coordinates use GCJ-02. The result includes `temperatureCelsius`, `relativeHumidity`, `skyCondition`, and `windSpeedMetersPerSecond`. The mock-backed integration test uses a real Dex Worker and Client with synthetic weather and no plan calls. If displaying live data in a product, follow the provider's attribution requirements.
