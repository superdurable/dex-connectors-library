# Paddle connector

The Go, Python, and TypeScript `getTransaction` Queries read one Paddle Billing transaction's ID, status, and ISO 4217 currency. Go also supports `listCustomers`, `getCustomer`, `createCustomer`, `updateCustomer`, and `archiveCustomer`. Customer creation has a three-send cap and reads back by unique email after an unknown outcome. Paddle Billing retains customers for recordkeeping, so archive replaces deletion. The language packages continue to implement only `getTransaction`.

## Connect

Sign in to the [Paddle Sandbox dashboard](https://sandbox-vendors.paddle.com) or the Live dashboard. Open **My account > Settings > Authentication > API keys > New API key**. Name the key, grant `transaction.read` plus `customer.read` and `customer.write` for Go customer operations, set an expiry, and save. Copy its complete secret value when shown once into the Dex Web connection's `api_key` field. The connector sends it in `Authorization: Bearer`. Revoke or rotate the key on the same API keys page. See [Paddle's authentication guide](https://developer.paddle.com/api-reference/about/authentication/).

Leave `baseUrl` blank for a `pdl_sdbx_apikey_` Sandbox key. For a `pdl_live_apikey_` Live key set `https://api.paddle.com`. Sandbox and Live keys are separate. `timeout` is a per-request Go duration and `maxResponseBytes` is a byte limit; blank uses each displayed default. Find the non-secret `txn_` ID on Paddle's Transactions page or in the create-transaction API response and pass it as `transactionId`; blank is invalid. See [Paddle's get transaction endpoint](https://developer.paddle.com/api-reference/transactions/get-transaction/).

## Examples and verification

Run the [Go](go/examples/transaction-inspection/README.md), [Python](python/examples/transaction_inspection/README.md), or [TypeScript](typescript/examples/transaction-inspection/README.md) transaction inspection Flow with the static `paddle` connection. The Go example also registers a customer listing Flow. Go-recorded conformance checks exact requests, branches, retries, and outputs; language replay covers only their declared operation. Local integration tests exercise real Dex with fixture providers. Real Paddle authorization, customer operations, and transaction retrieval have not been verified locally.
