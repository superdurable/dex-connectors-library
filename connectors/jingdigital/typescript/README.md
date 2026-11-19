# JINGdigital TypeScript connector

`@superdurable/connector-jingdigital` implements the read-only `listTags` Query. It first exchanges private app credentials for a temporary access token, then reads one page of ordinary tag IDs, names and levels. Credentials and the temporary token remain outside Dex Flow state, results and logs.

Create an application in the [JINGdigital management backend](https://www.jingdigital.com/) and configure its Appid and AppSecret in Dex Connections using the [authorization guide](../connector.yaml). HTTP 408/429/5xx and transport failures retry. Provider refusals, invalid responses and local defects select optional branches.

`examples/basic/` contains a runnable Dex Flow, Worker and Client. `npm run check` replays Go-recorded provider scenarios including the two-request token exchange. `npm run test:integration` against `dexcli dev -open=false` checks retry and persistence. Live provider authorization remains unverified without a user-controlled connection.
