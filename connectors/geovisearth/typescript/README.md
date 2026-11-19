# Geovisearth TypeScript connector

Read GEOVIS Earth current-weather observations for an official area code or coordinate.

Install `@superdurable/connector-geovisearth` and use `newProjectConnection` with the static connection name in the runnable [`examples/basic`](examples/basic) Flow. Configure the connection and provider credentials in Dex Web Connections. The operation `getCurrentWeather` resolves credentials for each call. Local conformance tests replay the Go provider scenarios; live provider authorization has not been verified.
