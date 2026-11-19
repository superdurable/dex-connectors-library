# UPYUN TypeScript connector

`@superdurable/connector-upyun` implements the `getServiceUsage` Query. In the [UPYUN console](https://console.upyun.com), open **Cloud Products → Cloud Storage → Service List** to find a storage service bucket, then **Account → Account Management → Operator Management** to create a read-authorized operator. Enter the bucket as non-secret configuration and the operator/password as credentials in Dex Connections. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for defaults. The password is hashed only in memory for request signing and never enters Flow state.

Temporary transport and HTTP 408/429/5xx outcomes retry. Rejection, malformed or oversized usage, and invalid credentials select optional branches that fail the Flow unless wired. Usage is returned in bytes.

`examples/basic/` contains a runnable Flow, Worker, and Client. `npm run check` replays Go-recorded scenarios. Run `npm run test:integration` with `dexcli dev -open=false` for real Dex retry and persistence verification. Live UPYUN authorization remains unverified without user-controlled credentials.
