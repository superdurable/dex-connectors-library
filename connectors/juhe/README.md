# Juhe Data current weather

The Go connector reads current conditions for one Chinese city from [Juhe Data API 73](https://www.juhe.cn/docs/api/id/73). The [official API PDF](https://www.juhe.cn/docs/download/pdf/73) documents the HTTPS `simpleWeather/query` endpoint, `city` and `key` parameters, response schema, and error codes. The connector returns only `result.city` and `result.realtime` fields; it discards the forecast array. Temperature, humidity, wind, and AQI may be empty according to the provider contract.

The API PDF documents GET and form POST, so the API key is sent in an HTTPS form POST body, never the URL, and remains a Dex credential. The [current API catalog](https://www.juhe.cn/docs/o3/19) lists 50 free calls per day, with higher membership limits. Transport failures, HTTP 429/5xx, and documented temporary service errors retry at most once to limit quota use. Authentication, authorization, invalid city, and exhausted quota are terminal branches.

See the [runnable Dex Flow](go/examples/current-weather/README.md). Mock tests do not verify live authorization, quota behavior, or provider responses.

The [TypeScript runnable Flow](typescript/examples/basic/README.md) uses the same provider contract and Go-recorded conformance scenarios.

The [Python package](python/) mirrors the Go Query and includes a [runnable Dex Worker/Client Flow](python/examples/current_weather/) and Go scenario replay. Real provider credentials and live API behavior remain unverified.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/juhe/CurrentWeatherWorker.java) and [Rust Worker](rust/examples/get_current_weather.rs) run `getCurrentWeather` on the static `juhe` connection. Start the Flow with `{"city":"北京"}`. The `fetched` branch returns the current conditions.

From this connector directory, start `dexcli dev -open=false` and set `DEX_FLOW_SERVICE_ADDRESS` to the Dex Server address. The Worker integration tests return a simulated HTTP 503, then verify the retried Step completes with the observation.

```sh
(cd java && ./gradlew build dexDevTest)
(cd rust && cargo test --locked --all-targets)
dexcli visualize java/examples/io/superdurable/dexconnectors/juhe/CurrentWeatherWorker.java --schema-version 2.0 --lint app --json --open=false --out /tmp/juhe-java
dexcli visualize rust/examples/get_current_weather.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/juhe-rust
```

The Java graph analyzer reports unresolved input and inherited Step handlers. The Rust analyzer cannot resolve the SDK-defined Query Step. The Worker tests execute both Steps.

For a provider-backed Worker, supply `JUHE_API_KEY` through its environment and run `./gradlew currentWeatherWorker` from `java/` or `cargo run --locked --example get_current_weather` from `rust/`. Provider requests can consume account quota.
