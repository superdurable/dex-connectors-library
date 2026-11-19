# ISAS Data TypeScript connector

`@superdurable/connector-istero` implements the read-only `getAccountStatus` Query. It returns the configured ISAS account ID and daily used/remaining API calls. The secret Token is resolved from Dex Connections and sent in a Bearer header; neither it nor the ISID enters Flow state.

Create an application Token in the [ISAS developer console](https://api.istero.com/) and enter it with its numeric ISID in Dex Connections as described in the [authorization guide](../connector.yaml). Leave timeout and maxResponseBytes blank for manifest defaults. Temporary transport and HTTP 408/429/5xx responses retry; provider refusals, malformed responses, and invalid credentials select optional failure branches.

`examples/basic/` is a runnable Dex Flow, Worker, and Client. `npm run check` replays Go-recorded provider scenarios. `npm run test:integration` against `dexcli dev -open=false` verifies retry and persistence. Live provider authorization remains unverified without a user-controlled connection.
