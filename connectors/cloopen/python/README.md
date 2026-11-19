# Cloopen Python connector

`dex-connector-cloopen` v0.1.0 implements the read-only `getBalance` Query.
It signs each HTTPS AccountInfo GET with the current UTC timestamp under
Cloopen's [REST authentication contract](https://doc.yuntongxun.com/p/5a533ac73b8496dd00dce025).
The 32-character main-account SID and auth token are resolved per attempt.
The token is used only for the uppercase MD5 `sig` query value; the
`Authorization` header contains Base64 of `SID:timestamp`. The Flow receives
only the provider's decimal balance string, with no inferred currency or units.

The Query selects `fetched`, `providerRejected`, `invalidResponse`, or
`defect`. HTTP 429, 408, 5xx, transport failures, concurrency code `111111`,
and internal `1110…` codes retry. Oversized or malformed responses select
`invalidResponse`. The [runnable balance Flow](examples/balance/README.md)
wires only `fetched`; optional branches fail its Flow.

The example's Dex Web setup has secret `account_sid` and `auth_token` fields,
plus `baseUrl`, `timeout`, and `maxResponseBytes`. The Cloopen management
console's account information page supplies the two credentials. Leave the
non-secret fields blank for manifest defaults. There is no operation-specific
configuration UI.

Go-recorded conformance covers the exact signature and headers, each branch,
429 and 5xx Retry, provider concurrency and internal retries, transport
failure, response size, and missing credentials. The example also runs against
a real Dex Worker with a fake provider. Live Cloopen authorization and the
console setup page remain unverified.
