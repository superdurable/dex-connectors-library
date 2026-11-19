# QWeather TypeScript connector

`@superdurable/connector-qweather` implements the read-only `getCurrentWeather` Step. It requests current weather from the account-specific QWeather API Host for decimal latitude and longitude and returns condition, temperature, time, and mandatory provider attributions. The host and API Key stay in Credentials and are never included in Flow input or output.

The [runnable Flow and Worker](examples/current-weather/) use the static `qweather` connection. At [QWeather Console](https://console.qweather.com/), open Projects > your project > Credentials > Add Credential > API KEY, and copy the complete key. Then open Settings > API Host and copy only the unique hostname, without scheme, path, or port. Leave timeout and response-size fields blank for displayed defaults. The Flow input supplies latitude and longitude in decimal degrees, for example 39.92 and 116.41 for Beijing.

Go-recorded conformance checks exact requests and all provider outcomes. The integration test runs a real Dex Worker and Client against a local provider that returns 503 once before success. Real QWeather authorization, available weather data, and quota behavior remain unverified.
