# Qiniu Kodo TypeScript connector

`@superdurable/connector-qiniu` implements the `listBuckets` Query with the same signed request and outcome branches as the Go connector. Create an AccessKey/SecretKey pair in [Qiniu Key Management](https://portal.qiniu.com/user/key), then enter both as secret credentials in Dex Connections. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for the manifest defaults. The keys stay outside Flow state; only the signed header reaches Qiniu.

Temporary transport and HTTP 408/429/5xx responses retry. Non-200 rejections select the optional `providerRejected` branch; malformed or oversized responses select `invalidResponse`; invalid credentials select `defect`. The optional branches fail the Flow unless wired to a business reaction.

`examples/basic/` contains a runnable Flow, Worker, and Client. `npm run check` replays the Go provider scenarios. Run `npm run test:integration` with a real `dexcli dev -open=false` server to verify retry and persistence. Live Qiniu authorization remains unverified without user-controlled credentials.
