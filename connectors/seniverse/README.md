# Seniverse current weather

The Go connector reads [current weather](https://docs.seniverse.com/api/weather/now.html) for one location. It returns the location ID/name/country, weather text/code, numeric temperature string and unit, and the provider's last update time. These weather fields are available to free and paid products; premium-only measures are intentionally omitted.

`GetCurrentWeatherInput.Location` accepts a Seniverse city ID, Chinese city name, pinyin name, or `latitude:longitude` pair documented in the [common parameters](https://docs.seniverse.com/api/start/common.html). The connector uses the configured language and temperature unit. The secret private key is sent as Seniverse's documented `key` query parameter over HTTPS; errors and Flow output omit it. Each request consumes one API access from the product quota.

See the [runnable Dex Flow](go/examples/current-weather/README.md) for connection setup and local verification. Real provider authorization and response have not been exercised by mock tests.

The company logo comes from Seniverse's [official brand assets](https://www.seniverse.com/brand).

The [Python package](python/) implements `getCurrentWeather` and its [runnable Flow](python/examples/current_query/) verifies retry and persistence with a real local Dex Worker and Client.
The [TypeScript package](typescript/) provides the same `GetCurrentWeather` operation, Go-recorded conformance, and a runnable Dex Worker and Client example.

For all five languages, an HTTP error with Seniverse code `AP010014` retries using `Retry-After`. Codes `AP010012` and `AP010013` select `providerRejected` with a quota-exhausted failure. If an observation contains the private key, the query selects `invalidResponse`.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/seniverse/CurrentWeatherWorker.java) and [Rust Worker](rust/examples/get_current_weather.rs) run `getCurrentWeather` on the static `seniverse` connection. Start the Flow with `{"location":"Wuhan"}`. The `fetched` branch returns the current conditions.

From this connector directory, start `dexcli dev -open=false` and set `DEX_FLOW_SERVICE_ADDRESS` to the Dex Server address. The Worker integration tests return a simulated HTTP 503, then verify the retried Step completes with the observation.

```sh
(cd java && ./gradlew build dexDevTest)
(cd rust && cargo test --locked --all-targets)
dexcli visualize java/examples/io/superdurable/dexconnectors/seniverse/CurrentWeatherWorker.java --schema-version 2.0 --lint app --json --open=false --out /tmp/seniverse-java
dexcli visualize rust/examples/get_current_weather.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/seniverse-rust
```

The Java graph analyzer reports unresolved input and inherited Step handlers. The Rust analyzer cannot resolve the SDK-defined Query Step. The Worker tests execute both Steps.

For a provider-backed Worker, supply `SENIVERSE_PRIVATE_KEY` through its environment and run `./gradlew currentWeatherWorker` from `java/` or `cargo run --locked --example get_current_weather` from `rust/`. Provider requests can consume account quota.
