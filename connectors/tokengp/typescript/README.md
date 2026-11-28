# TokenGP TypeScript connector

`@superdurable/connector-tokengp` implements the read-only `listModels` Query. It resolves provider credentials for each call and returns only the fields declared by the generated contract. Provider 429, 503, transport and other retryable failures use Dex retry; provider refusal, malformed response and local defects select optional branches.

`examples/basic/` contains a runnable Dex Flow, Worker and Client. `npm run check` replays all Go-recorded provider scenarios. Run `npm run test:integration` against `dexcli dev -open=false` for real Dex retry and persistence verification. Live provider authorization remains unverified without a user-controlled connection.

The query preserves repeated IDs and their provider order. ID length is limited to 256 UTF-8 bytes.
