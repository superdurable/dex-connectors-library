# WAYZ current weather

This Go connector reads one live weather observation for a six-digit Chinese city code through the [WAYZ weather API](https://lothub.newayz.com/pdf/api-weather.pdf). `GetCurrentWeatherInput.CityCode` accepts an administrative code such as `420100` for Wuhan. The output contains province, city, provider city code, weather, Celsius temperature, wind, relative humidity, and report time.

Register and apply for an application at [LoTBoard](https://lotboard.newayz.com/). After approval, enable the Weather API and copy the application's AccessKey into the Dex Web `newayz` connection. The key is sent only to the documented HTTPS endpoint and is excluded from Flow outputs and errors. Queries may consume provider quota. Retryable transport and server failures use a bounded retry budget.

See the [runnable Dex Flow](go/examples/current-weather/README.md) for local mock verification. A real WAYZ account and live API call have not been exercised.

The [Python package](python/README.md) implements the same Query with a checked-in Dex Worker and Client example. Its Go-recorded provider scenarios and local Dex retry path are verified; live provider authorization remains unverified.
The [TypeScript package](typescript/README.md) exposes `getCurrentWeather` and includes a runnable Dex Flow, Go scenario replay, and a local provider integration test.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/newayz/CurrentWeatherWorker.java) and [Rust Worker](rust/examples/get_current_weather.rs) run `getCurrentWeather` on the static `newayz` connection. Start the Flow with `{"cityCode":"420100"}`. The `fetched` branch returns the current conditions.

From this connector directory, start `dexcli dev -open=false` and set `DEX_FLOW_SERVICE_ADDRESS` to the Dex Server address. The Worker integration tests return a simulated HTTP 503, then verify the retried Step completes with the observation.

```sh
(cd java && ./gradlew build dexDevTest)
(cd rust && cargo test --locked --all-targets)
dexcli visualize java/examples/io/superdurable/dexconnectors/newayz/CurrentWeatherWorker.java --schema-version 2.0 --lint app --json --open=false --out /tmp/newayz-java
dexcli visualize rust/examples/get_current_weather.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/newayz-rust
```

The Java graph analyzer reports unresolved input and inherited Step handlers. The Rust analyzer cannot resolve the SDK-defined Query Step. The Worker tests execute both Steps.

For a provider-backed Worker, supply `NEWAYZ_ACCESS_KEY` through its environment and run `./gradlew currentWeatherWorker` from `java/` or `cargo run --locked --example get_current_weather` from `rust/`. Provider requests can consume account quota.
