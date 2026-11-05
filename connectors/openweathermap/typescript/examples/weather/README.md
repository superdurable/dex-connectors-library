# Current-weather Flow

`WeatherFlow` reads one OpenWeatherMap current-weather result using the static `openweathermap` connection. `input.example.json` contains only nonsecret coordinates and units. In Dex Web Connections, follow the OpenWeatherMap API-key guide, save the key in the private connection, and leave the base URL and safety limits blank to use their displayed defaults.

Start `dexcli dev` and export `DEX_FLOW_SERVICE_ADDRESS` with its gRPC address. Set `DEX_CONNECTOR_CONFIG_FILE` to the private `connections.json` created by Dex Web. From `connectors/openweathermap/typescript`, run:

```sh
npm run example:weather
```

In another terminal with the same environment, run:

```sh
npm run example:weather:start -- examples/weather/input.example.json
```

For the real Dex integration test against a local fixture provider, use an unused Dex port and run:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:23401 npm run test:integration
```

Generate and lint Flow Definition Graph 2.0 with Dex CLI 1.28.0 or newer:

```sh
npm run build
npm install --prefix examples/weather --no-save --no-package-lock --ignore-scripts .
cd examples/weather
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

The provider fixture verifies Dex execution and optional-branch failure. Live provider behavior remains unverified.
