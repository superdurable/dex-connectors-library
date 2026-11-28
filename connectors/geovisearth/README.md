# GEOVIS Earth connector

[GEOVIS Earth](https://open.geovisearth.com/collect/weather?docId=0) is a Chinese weather platform jointly operated by 中科星图股份有限公司 and 中科星图数字地球合肥有限公司. Its [current weather API](https://open.geovisearth.com/support/document?docId=220&sid=44&source=%E5%A4%A9%E6%B0%94API) documents `GET https://api.open.geovisearth.com/v2/cn/area/basic` with service token and area-code or longitude,latitude input. This connector returns one basic observation: weather description/code, Celsius temperature, humidity, wind, surface pressure, observation time, and area code if supplied. Each request consumes platform call quota; retries are limited to three attempts.

## Connection

Register and verify an account on the [GEOVIS Earth open platform](https://open.geovisearth.com/). Follow the weather API developer guide and apply for a token with access to **全国天气实况（6要素）**. Put that token in the secret `token` field. The provider requires it in the URL query string; the connector never logs or returns it. Avoid HTTP debugging that logs full request URLs. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. The connector permits only the documented HTTPS API host.

`GetCurrentWeatherInput.Location` accepts an area code such as `WTX_CH101300201`, or decimal `longitude,latitude`. For coordinates, `areaCode` may be blank in the response. The optional `providerRejected`, `invalidResponse`, and `defect` branches remain unwired in the example. HTTP 429, 408, 5xx, and transport errors retry within the three-attempt budget; nonzero provider status selects `providerRejected`.

## Example

The [current-weather Flow](go/examples/current-weather/) is a runnable Worker with static Dex connection name `geovisearth`. Its mock is synthetic and is verified with a real Dex Worker and Client. Live authorization, location mapping, quota handling, and current observations remain unverified without a platform token.

## TypeScript

The [TypeScript package](typescript/) provides the `getCurrentWeather` Query and a [runnable Dex Worker/Client example](typescript/examples/basic/). It replays the Go-recorded provider scenarios and keeps the token outside Flow state.

## Python

The first Python package is [`python/`](python/README.md). It implements the same read-only Query and replays the Go-recorded provider scenarios. Its runnable Flow includes a real Dex Worker and Client integration test.

The query accepts the documented HTTPS origin. An observation that echoes the connection token in any returned text field selects `invalidResponse`.

## Java and Rust

The [Java Worker](java/examples/io/superdurable/dexconnectors/geovisearth/CurrentWeatherWorker.java) and [Rust Worker](rust/examples/get_current_weather.rs) run `getCurrentWeather` on the static `geovisearth` connection. Start the Flow with `{"location":"WTX_CH101300201"}`. The `fetched` branch returns the current conditions.

From this connector directory, start `dexcli dev -open=false` and set `DEX_FLOW_SERVICE_ADDRESS` to the Dex Server address. The Worker integration tests return a simulated HTTP 503, then verify the retried Step completes with the observation.

```sh
(cd java && ./gradlew build dexDevTest)
(cd rust && cargo test --locked --all-targets)
dexcli visualize java/examples/io/superdurable/dexconnectors/geovisearth/CurrentWeatherWorker.java --schema-version 2.0 --lint app --json --open=false --out /tmp/geovisearth-java
dexcli visualize rust/examples/get_current_weather.rs --schema-version 2.0 --lint app --json --open=false --out /tmp/geovisearth-rust
```

The Java graph analyzer reports unresolved input and inherited Step handlers. The Rust analyzer cannot resolve the SDK-defined Query Step. The Worker tests execute both Steps.

For a provider-backed Worker, supply `GEOVISEARTH_TOKEN` through its environment and run `./gradlew currentWeatherWorker` from `java/` or `cargo run --locked --example get_current_weather` from `rust/`. Provider requests can consume account quota.
