# QWeather connector

This connector implements the read-only [current weather API](https://dev.qweather.com/en/docs/api/weather/weather-current/) for decimal latitude and longitude. It calls the account-specific HTTPS API Host with `X-QW-Api-Key` authentication and returns current condition, temperature, observation time, and provider attributions. The host is part of QWeather authentication and is stored with the key as a Dex credential. Neither appears in Flow results, receipts, Attributes, or logs.

Create a project and API KEY credential in the [QWeather Console](https://console.qweather.com/), then copy your account's API Host from Settings. QWeather requires its dedicated API Host; legacy shared hosts are being retired. Invalid coordinates fail locally, while provider errors select `providerRejected`. Transient failures and every HTTP 429, including the monthly request limit, retry up to three times and honor `Retry-After`. QWeather states that API KEY authentication will have a daily call-volume limit from 2027-02-01; this connector currently uses the documented API KEY method.

The runnable [current-weather example](go/examples/current-weather/README.md) demonstrates the generated operation-specific Step and static `qweather` connection. Real QWeather authorization remains to be tested with a user-controlled Dex Web Connection.

The [Python package](python/) implements `getCurrentWeather` and its [runnable Flow](python/examples/current_query/) verifies retry and persistence with a real local Dex Worker and Client.
The [TypeScript package](typescript/) provides the same `GetCurrentWeather` operation, Go-recorded conformance, and a runnable Dex Worker and Client example.

A monthly quota response retries with `QUOTA_EXHAUSTED`. Other HTTP 429 responses retry with `RATE_LIMIT`. Both honor `Retry-After`. The query rejects a response that echoes the account API host or key in a returned text field.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/qweather/CurrentWeatherWorker.java) and [Rust Worker](rust/examples/get_current_weather.rs) run `getCurrentWeather` on the static `qweather` connection. Start the Flow with `{"latitude":39.92,"longitude":116.41}`. The `found` branch returns the current conditions.

From this connector directory, start `dexcli dev -open=false` and set `DEX_FLOW_SERVICE_ADDRESS` to the Dex Server address. The Worker integration tests return a simulated HTTP 503, then verify the retried Step completes with the observation.

```sh
(cd java && ./gradlew build dexDevTest)
(cd rust && cargo test --locked --all-targets)
dexcli visualize java/examples/io/superdurable/dexconnectors/qweather/CurrentWeatherWorker.java --schema-version 2.0 --lint app --json --open=false --out /tmp/qweather-java
dexcli visualize rust/examples/get_current_weather.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/qweather-rust
```

The Java graph analyzer reports unresolved input and inherited Step handlers. The Rust analyzer cannot resolve the SDK-defined Query Step. The Worker tests execute both Steps.

For a provider-backed Worker, supply `QWEATHER_API_HOST` and `QWEATHER_API_KEY` through its environment and run `./gradlew currentWeatherWorker` from `java/` or `cargo run --locked --example get_current_weather` from `rust/`. Provider requests can consume account quota.
