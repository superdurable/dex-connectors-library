# Caiyun Weather current weather

This connector reads [Caiyun Weather v2.6 realtime observations](https://docs.caiyunapp.com/weather-api/v2/v2.6/1-realtime.html) for one longitude and latitude. Mainland China coordinates must use GCJ-02; other locations, including Hong Kong and Taiwan, use WGS84. Output contains Celsius temperature, relative humidity (0–1), a sky condition code, and wind speed (m/s).

The connector uses [App Key and App Secret signing](https://docs.caiyunapp.com/weather-api/v2/v2.6/auth.html) through HTTPS. Both are secret Dex connection fields. The App Key appears in the provider URL path as required; the App Secret remains local. Neither is returned in Flow results. Requests consume [plan calls](https://docs.caiyunapp.com/weather-api/billing.html); retries are limited to two attempts because an ambiguous failure may consume an extra call. Every HTTP 429, including QPS and quota limits, retries and honors `Retry-After`; when retries run out the Flow fails.

See the [runnable Flow](go/examples/current-weather/README.md). The mock uses synthetic Beijing weather and no provider credentials. Live signed authorization and weather data have not been verified.

The [Python package](python/) includes a runnable current-weather Flow and Go-recorded conformance replay.

The [TypeScript package](typescript/README.md) includes a [runnable Flow](typescript/examples/current-weather/README.md) and Go-recorded conformance replay.

Each retry resolves the signing credentials again and generates a fresh nonce. A successful HTTP response that echoes the app key or secret in the sky condition selects `invalidResponse`.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/caiyun/CurrentWeatherWorker.java) and [Rust Worker](rust/examples/get_current_weather.rs) run `getCurrentWeather` on the static `caiyun` connection. Start the Flow with `{"latitude":39.92,"longitude":116.41}`. The `fetched` branch returns the current conditions.

From this connector directory, start `dexcli dev -open=false` and set `DEX_FLOW_SERVICE_ADDRESS` to the Dex Server address. The Worker integration tests return a simulated HTTP 503, then verify the retried Step completes with the observation.

```sh
(cd java && ./gradlew build dexDevTest)
(cd rust && cargo test --locked --all-targets)
dexcli visualize java/examples/io/superdurable/dexconnectors/caiyun/CurrentWeatherWorker.java --schema-version 2.0 --lint app --json --open=false --out /tmp/caiyun-java
dexcli visualize rust/examples/get_current_weather.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/caiyun-rust
```

The Java graph analyzer reports unresolved input and inherited Step handlers. The Rust analyzer cannot resolve the SDK-defined Query Step. The Worker tests execute both Steps.

For a provider-backed Worker, supply `CAIYUN_APP_KEY` and `CAIYUN_APP_SECRET` through its environment and run `./gradlew currentWeatherWorker` from `java/` or `cargo run --locked --example get_current_weather` from `rust/`. Provider requests can consume account quota.
