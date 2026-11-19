# Youzan TypeScript connector

`@superdurable/connector-youzan` implements the `getShopBasic` Query for the shop authorized by the connection token. Use the [Youzan application console](https://doc.youzanyun.com/) to obtain a shop `access_token`; enter the opaque value as a secret credential in Dex Connections. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for manifest defaults. Youzan requires the token in the request URL; the connector never persists it in Flow state or logs.

Transport and HTTP 408/429/5xx failures retry; Youzan system code 161400100 also retries within the metered operation budget. Rejection, malformed data, and invalid credentials select optional branches that fail the Flow unless wired.

`examples/basic/` contains a runnable Flow, Worker, and Client. `npm run check` replays Go-recorded scenarios. Run `npm run test:integration` with `dexcli dev -open=false` to verify real Dex retry and persistence. Live Youzan authorization remains unverified without a user-controlled token.
