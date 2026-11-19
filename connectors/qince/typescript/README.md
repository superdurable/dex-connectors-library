# Qince TypeScript connector

`@superdurable/connector-qince` implements the read-only `listDepartments` Query. It resolves provider credentials for each call and returns only the fields declared by the generated contract. Provider 429, 503, transport and other retryable failures use Dex retry; provider refusal, malformed response and local defects select optional branches.

`examples/basic/` contains a runnable Dex Flow, Worker and Client. `npm run check` replays all Go-recorded provider scenarios. Run `npm run test:integration` against `dexcli dev -open=false` for real Dex retry and persistence verification. Live provider authorization remains unverified without a user-controlled connection.
