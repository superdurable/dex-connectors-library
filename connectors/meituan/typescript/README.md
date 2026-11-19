# Meituan TypeScript connector

`@superdurable/connector-meituan` implements the read-only `queryOrderStatus` Query. It resolves provider credentials for each call and returns the generated contract. Provider 429, 503, transport, and other temporary failures use Dex retry; provider refusal, malformed response, and local defects select optional branches.

`examples/basic/` contains a runnable Dex Flow, Worker, and Client using the static `meituan` connection. Run `npm run check` for Go-recorded provider replay. Run `npm run test:integration` against `dexcli dev -open=false` for real Dex retry and persistence verification. Live provider authorization remains unverified without a user-controlled connection.
