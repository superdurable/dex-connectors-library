# Wise Platform connector

The Go, Python, and TypeScript `getTransfer` Query reads a Wise transfer's numeric ID, status, and source currency. Recipient, customer, and payment details stay out of Dex Results.

## Connect

For a Wise business account, follow the [personal API token guide](https://docs.wise.com/guides/developer/auth-and-security/personal-api-token): sign in at [wise.com](https://wise.com), open **Your Account > Connect and manage apps > API tokens > Add new token**, complete two-step verification, and copy the opaque secret into Dex Web `api_token`. The connector sends it in the `Authorization: Bearer` header. A partner OAuth user token may also be entered into this secret field.

Leave `baseUrl` blank for Production at `https://api.wise.com/2026Q4`; use `https://api.wise-sandbox.com/2026Q4` for Sandbox. Match the token to the environment. `timeout` is a per-request Go duration and `maxResponseBytes` is a byte limit; blank uses the displayed defaults. Find a positive numeric transfer ID in Wise transfer details or an API response and pass it as the `transferId` string. The three implementations currently accept IDs through 9007199254740991 to preserve exact JavaScript integer decoding. See [Get a transfer by ID](https://docs.wise.com/api-reference/transfer/transferget).

## Examples and verification

Run the [Go](go/examples/transfer-inspection/README.md), [Python](python/examples/transfer_inspection/README.md), or [TypeScript](typescript/examples/transfer-inspection/README.md) transfer inspection Flow with the static `wise` connection. Go-recorded conformance checks requests, branches, retries, and outputs across all three languages. Local integration tests exercise real Dex with fixture providers. Real Wise authorization and transfer retrieval remain unverified.
