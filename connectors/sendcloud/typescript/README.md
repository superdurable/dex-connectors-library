# SendCloud TypeScript connector

`@superdurable/connector-sendcloud` implements the `getAccountBalance` Query, including account balances and daily API quota without contact details. In the [SendCloud console](https://www.sendcloud.net), open **发送设置 → API USER**, create or select an API_USER, and generate its paired API_KEY. Enter both as secret credentials in Dex Connections. Blank `baseUrl`, `timeout`, and `maxResponseBytes` retain the manifest defaults.

The connector submits credentials in an HTTPS form body, never a URL or Flow state. Temporary transport and HTTP 408/429/5xx outcomes retry; SendCloud business rate code 50000 also retries. Provider rejection, malformed response, and invalid local credentials select optional branches that fail the Flow unless wired.

`examples/basic/` contains a runnable Flow, Worker, and Client. `npm run check` replays Go-recorded scenarios. Run `npm run test:integration` with `dexcli dev -open=false` for real Dex retry and persistence verification. Live SendCloud authorization remains unverified without user-controlled credentials.
