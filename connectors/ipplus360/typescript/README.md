# IPPlus360 TypeScript connector

`@superdurable/connector-ipplus360` implements the read-only `lookupIPLocation` Query. It accepts a public IPv4 address and returns coarse country, province, city, district, and operator fields. A secret API key is resolved from Dex Connections and sent to IPPlus360 over HTTPS; it never enters Flow state or results.

Obtain a key through the provider account and API guidance linked in the [connector authorization guide](../connector.yaml). Leave timeout and maxResponseBytes blank for manifest defaults. Temporary transport and HTTP 408/429/5xx responses retry; provider refusals, malformed responses, and invalid local input select optional failure branches.

`examples/basic/` is a runnable Dex Flow, Worker, and Client. `npm run check` replays Go-recorded provider scenarios. `npm run test:integration` against `dexcli dev -open=false` verifies retry and persistence. Live provider authorization remains unverified without a user-controlled connection.
