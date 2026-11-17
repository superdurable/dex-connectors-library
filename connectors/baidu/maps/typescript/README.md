# Baidu Maps TypeScript connector

`@superdurable/connector-baidu-maps` implements the Dex `geocodeAddress` Query. It sends a structured Chinese address to Baidu Maps Geocoding API v3 and returns BD-09 longitude, latitude, precision, confidence, comprehension, and level. The server AK remains in the host credential provider; Baidu requires it on the provider request URL.

Create a server-side AK at [Baidu Maps Application Management](https://lbs.baidu.com/apiconsole/key): 应用管理 > 我的应用 > 创建应用. Select 服务端, enable geocoding, choose IP whitelist validation, and allow the Dex Worker egress IP. In Dex Web Connections, create the static `baidu-maps` connection and set `access_key`. The `baseUrl`, `timeout`, and `maxResponseBytes` fields have safe defaults in the manifest. Leave them blank unless using an approved proxy or changing request limits.

The checked-in [address lookup example](examples/address-lookup/README.md) is the consuming application. It binds `newProjectConnection(project, 'baidu-maps')` to a Worker and uses `newGeocodeAddressStep`. Only `geocoded` is wired; optional business failures fail the Flow. Run `npm ci`, `npm run check`, and `npm run test:integration` with a local `dexcli dev` server. The integration uses a local HTTP provider; authorization against a live Baidu account remains unverified.

`geocodeAddress` retries 429, 408, 5xx, provider status 1, and transport errors. It selects `providerRejected` for definite provider rejections, `invalidResponse` for malformed or oversized data, and `defect` for invalid local input or credentials. The eight Go-recorded scenarios in `conformance/connectors/baidu/maps/scenarios.json` are replayed by the TypeScript test suite.
