# Dieyuyun balance for Dex TypeScript

`@superdurable/connector-dieyuyun` reads the available CNY balance for the tenant associated with a Wuliang AI API Key. The `getAccountBalance` Query accepts `{}` and returns `{ availableCNY }`. The balance remains decimal text, preserving every digit from the provider's JSON number. `fetched` is the happy branch; `providerRejected`, `invalidResponse`, and `defect` are optional terminal branches. Transport, 429, and 5xx failures return Retry.

## Connection

Create a Dex Web Connection named `dieyuyun`. In the [Wuliang AI console](https://open-console.dieyuyun.com/), open **API Keys → Key Management → Create API Key** and copy the one-time-shown secret into `api_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. The [official Balance API](https://open.dieyuyun.com/guide/api-reference.html#balance-%E4%BD%99%E9%A2%9D%E6%9F%A5%E8%AF%A2) is GET `/v1/balance` with Bearer authorization.

Only the available CNY amount enters Flow output; the Key name, ID, budget details, and credential remain private.

## Runnable Flow

The [balance example](examples/balance/README.md) includes a Worker and Client and persists the decimal balance. Install with `npm install`, run `npm run example:worker`, then `npm run example:start` after configuring Dex.

Provider conformance replays 15 Go-recorded scenarios, including high precision and scientific notation. The integration test runs the same Flow through a real Dex Worker against a local provider fixture. Real Wuliang AI authorization and production balance lookup remain unverified without a user-owned connection.
