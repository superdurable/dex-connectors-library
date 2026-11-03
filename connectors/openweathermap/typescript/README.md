# OpenWeatherMap TypeScript connector

The `@superdurable/connector-openweathermap` package provides `getCurrentWeather` and `getForecast` Dex Queries. Both accept decimal latitude and longitude. `units` is `standard`, `metric`, or `imperial`; blank uses `standard`. The connector reads the API key from a named Dex connection and sends it as OpenWeatherMap's `appid` query parameter.

## Runnable weather Flow

[The weather example](examples/weather/flow/workflow.ts) calls `getCurrentWeather` from a Dex Worker using the static connection name `openweathermap`. Only `found` is wired. The `notFound`, `providerRejected`, `invalidResponse`, and `defect` outcomes fail the Flow. The input contains coordinates and units, never an API key.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. In Dex Web Connections, create the `openweathermap` connection and follow the API-key guide at <https://home.openweathermap.org/api_keys>. Keep the secret in the connection. Set `DEX_CONNECTOR_CONFIG_FILE` to the private Dex Web `connections.json` file, then run these commands from this package directory:

```sh
npm run example:weather
npm run example:weather:start -- examples/weather/input.example.json
```

The first command runs the Worker; run the second in another terminal. The starter prints the current-weather result. The package's [example README](examples/weather/README.md) has the strict Flow Definition Graph and integration-test commands.

Live OpenWeatherMap authorization and API responses have not been verified. The local integration test uses a fixture provider and real Dex Worker/Client.
