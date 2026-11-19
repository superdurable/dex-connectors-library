# Plaid connector

The Go, Python, and TypeScript `getAccounts` Query returns account IDs, types, and subtypes for one linked Plaid Item. Go also provides `getItem` to read the Item ID, institution ID, and enabled products, plus `removeItem` to disconnect that Item. Names, masks, balances, and credentials stay out of Dex Results. Plaid's `ITEM_NOT_FOUND` error for a removed or unknown Item selects `notFound` for reads; removal treats it as an already disconnected Item. Other Plaid errors, including `INVALID_ACCESS_TOKEN`, select `providerRejected`.

## Connect

Sign in to the [Plaid Dashboard](https://dashboard.plaid.com), open **Build > Keys**, and copy the client ID and the secret for the selected environment into Dex Web `client_id` and `secret`. Link one Item through Plaid Link, exchange its public token with `POST /item/public_token/exchange`, then store the returned access token in secret `access_token`. One Dex connection binds one Item token. Pass the non-secret Item ID returned by the exchange as Flow input `itemId`. See [Plaid API overview](https://plaid.com/docs/api/) and [Accounts API](https://plaid.com/docs/api/accounts/).

Leave `baseUrl` blank for Sandbox at `https://sandbox.plaid.com`, or use `https://production.plaid.com` for Production. Match the environment of the secret and Item token. `timeout` is a per-request Go duration and `maxResponseBytes` is a byte limit; blank uses the displayed defaults. Plaid's `/accounts/get` returns cached account information; this connector exposes no balances.

## Examples and verification

Run the [Go](go/examples/accounts-inspection/README.md), [Python](python/examples/accounts_inspection/README.md), or [TypeScript](typescript/examples/accounts-inspection/README.md) Flow with the static `plaid` connection. The Go example also reads the Item after the accounts; `removeItem` is an explicit offboarding action that invalidates this connection's access token and should be placed in its own Flow when needed. Go-recorded conformance checks requests, branches, retries, and outputs; unchanged language packages replay their declared `getAccounts` scenarios. Local integration tests exercise real Dex with fixture providers. Real Plaid authorization, account retrieval, and removal remain unverified.
