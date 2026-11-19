# West TypeScript connector

`@superdurable/connector-west` reads one reseller domain page, returning domain names and expiration dates. It signs each read with the current username, API password, and Unix millisecond timestamp. The password stays outside Flow input and output. Responses are decoded as GBK, matching West.cn.

`examples/basic/` runs a Dex Flow, Worker, and Client. `npm run check` replays Go provider scenarios; `npm run test:integration` uses a synthetic provider with real Dex retry and persistence. Live reseller authorization is unverified.
