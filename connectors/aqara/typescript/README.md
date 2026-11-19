# Aqara TypeScript connector

`@superdurable/connector-aqara` provides the read-only `listDevices` Dex Step. It reads one bounded page of device identity and online state through Aqara's mainland China [v3 device API](https://opendoc.aqara.com/docs/%E4%BA%91%E5%BC%80%E5%8F%91/API%E6%96%87%E6%A1%A3/%E8%AE%BE%E5%A4%87%E7%AE%A1%E7%90%86%E6%8E%A5%E5%8F%A3.html).

Use the [runnable Flow and Worker](examples/device-list/) with a static `aqara` connection. In Dex Web Connections, create an approved project at [Aqara Developer Platform](https://developer.aqara.com), then enter its AppID, KeyID, AppKey, and authorized access token. The AppKey and token remain private and are resolved for each request. The connector signs requests using the documented [Aqara signature rules](https://opendoc.aqara.com/en/docs/developmanual/apiIntroduction/signGenerationRules.html). The endpoint, timeout, and response limit have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests, signatures, and outcomes. The integration test exercises a real Dex Worker and Client with a local provider that returns 503 before succeeding. Real Aqara authorization, token refresh, and device discovery remain unverified without a user-controlled account and approved project.
