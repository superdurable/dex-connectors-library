# Wise Platform connector

The Go, Python, and TypeScript `getTransfer` Query reads a Wise transfer's numeric ID, status, and source currency. Go also provides `listProfiles`, `getProfile`, `listRecipients`, `getExchangeRates`, `getQuote`, `createTransfer`, and `cancelTransfer`. Profile and recipient Results contain only IDs and selected metadata; transfer Results omit recipient, customer, and payment details.

## Connect

For a Wise business account, follow the [personal API token guide](https://docs.wise.com/guides/developer/auth-and-security/personal-api-token): sign in at [wise.com](https://wise.com), open **Your Account > Connect and manage apps > API tokens > Add new token**, complete two-step verification, and copy the opaque secret into Dex Web `api_token`. The connector sends it in the `Authorization: Bearer` header. A partner OAuth user token may also be entered into this secret field.

Leave `baseUrl` blank for Production at `https://api.wise.com/2026Q4`; use `https://api.wise-sandbox.com/2026Q4` for Sandbox. Match the token to the environment. `timeout` is a per-request Go duration and `maxResponseBytes` is a byte limit; blank uses the displayed defaults. Find a positive numeric transfer ID in Wise transfer details or an API response and pass it as the `transferId` string. The three implementations currently accept IDs through 9007199254740991 to preserve exact JavaScript integer decoding. See [Get a transfer by ID](https://docs.wise.com/api-reference/transfer/transferget).

## Go operations

`listProfiles` and `getProfile` return profile IDs and types. `listRecipients` returns recipient IDs, currencies, and active flags without bank details. `getExchangeRates` reads a current ISO 4217 pair, and `getQuote` reads an authenticated quote by profile ID and UUID. All reads are safe to retry.

`createTransfer` creates an unfunded transfer from an authenticated quote and recipient ID. The connector derives `customerTransactionId` from the stable Dex Call ID; Wise documents that repeating the same identifier prevents duplicate transfers. Optional `reference` and `transferPurpose` values must come from Wise's transfer requirements for the chosen corridor. `cancelTransfer` requests cancellation of an unfunded transfer and confirms a `cancelled` status after a conflict. Both mutations are safe to repeat for the same logical request. Funding is a separate Wise action.

See [Wise profiles](https://docs.wise.com/guides/developer/auth-and-security/personal-api-token), [recipient listing](https://docs.wise.com/guides/product/send-money/recipients/list-create-recipient), [rates](https://docs.wise.com/api-reference/rate/rateget), [quotes](https://docs.wise.com/api-reference/quote/quoteget), [transfer creation](https://docs.wise.com/guides/product/send-money/transfers/standard-api-transfers), and [cancellation](https://docs.wise.com/api-reference/transfer/transfercancel).

## Examples and verification

Run the [Go](go/examples/transfer-inspection/README.md), [Python](python/examples/transfer_inspection/README.md), or [TypeScript](typescript/examples/transfer-inspection/README.md) transfer inspection Flow with the static `wise` connection. The Go example also registers `WiseProfileInspection`. Go-recorded conformance checks requests, branches, retries, and outputs; Python and TypeScript replay the `getTransfer` scenarios they implement. Local integration tests exercise real Dex with fixture providers. Real Wise authorization and provider responses remain unverified.
