# Seniverse TypeScript connector

`@superdurable/connector-seniverse` implements the read-only `getCurrentWeather` Step. It calls `GET /v3/weather/now.json` for one location and returns location identity, condition, temperature, unit, and observation time. The private key is sent only to Seniverse in the required request query; it is never included in Flow input or output.

The [runnable Flow and Worker](examples/current-weather/) use the static `seniverse` connection. At [Seniverse](https://www.seniverse.com/login), open 产品管理, select a Weather API product with 天气实况 access, then open 密钥 and copy the 私钥 into Dex Web Connections. Use the private key, not the public uid. Leave the API origin, timeout, response limit, language, and unit blank for displayed defaults. The Flow input supplies a city ID, name, pinyin name, or coordinate pair.

Go-recorded conformance checks all provider outcomes and exact requests. The integration test runs a real Dex Worker and Client against a local provider that returns 503 once before success. Real Seniverse authorization and quota behavior remain unverified.
