# Juhe current weather Flow

This runnable Flow uses the static `juhe` connection to read one city's current weather. The `fetched` branch stores the result in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/juhe/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/current-weather/flow/workflow.go --schema-version 2.0 --json --out ./build/current-weather
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override juhe=<artifact-directory>`. In Dex Web **Connections**, create the `juhe` connection. At [Juhe API 73](https://www.juhe.cn/docs/api/id/73), choose **申请接口**, complete the requested certification, and copy the key from **个人中心 > 我的数据 > 天气预报** into the secret `api_key` field. Leave base URL, timeout, and response size blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/current-weather
```

In Dex Web **Start Flow**, choose `JuheCurrentWeather` and enter a unique Flow ID:

```json
{"city":"北京"}
```

The output includes `city`, `weatherText`, `weatherCode`, and available current observations. The mock-backed integration test uses a real Dex Worker and Client with synthetic sample data and no provider credential.
