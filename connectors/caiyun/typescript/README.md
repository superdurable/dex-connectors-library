# Caiyun TypeScript connector

`@superdurable/connector-caiyun` provides `getCurrentWeather`, a read-only Query for one longitude and latitude. It returns metric temperature, humidity, sky condition, and wind speed. The host resolves the Caiyun App Key and App Secret for each call; they never enter Flow input or persisted output. Caiyun requires the App Key in the signed provider URL path.

The runnable [current weather example](examples/current-weather/README.md) binds the static `caiyun` connection. Configure credentials in Dex Web Connections using the guide in `connector.yaml`. The package replays Go provider scenarios for signed paths and headers, successful observations, rejection, invalid data, rate limits, oversized responses, and transport failure. Its integration test exercises a real Dex Worker and Client with a local provider fake. Real Caiyun authorization and observations have not been verified.

Install with `npm install @superdurable/connector-caiyun`. `CaiyunClient` validates its origin and limits during construction. `newProjectConnection` loads the named connection. Normal Flows use the generated `newGetCurrentWeatherStep` factory; `CaiyunClientOptions` can inject an approved sender, clock, or nonce generator for tests.
