# APISpace TypeScript connector

`@superdurable/connector-apispace` provides the read-only `getCurrentWeather` Dex Step. It reads one city's current observations from the [APISpace Weather Forecast Query API](https://www.apispace.com/eolink/api/456456/apiDocument).

Use the [runnable Flow and Worker](examples/current-weather/) with a static `apispace` connection. In Dex Web Connections, subscribe to Weather Forecast Query (product 456456), then open Console > Access Control and enter the API key in the protected `api_token` field. The city `areaCode` comes from the product's Domestic Cities attachment. The endpoint, timeout, and response limit have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The integration test exercises a real Dex Worker and Client with a local provider that returns 503 before succeeding. Real APISpace authorization and current weather remain unverified without a user-controlled subscription and API key.
