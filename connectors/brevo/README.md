# Brevo connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/brevo/go@v0.2.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/brevo`.

Brevo contact operations for Dex. The module uses Brevo's public v3 REST API and a secret API key. Its first release is `v0.1.0`.

| Operation | Method | Result |
| --- | --- | --- |
| `createOrUpdateContact` | `POST /v3/contacts` with `updateEnabled: true` | `saved` after HTTP 201 or 204 |
| `getContact` | `GET /v3/contacts/{email}` | `found` after HTTP 200; `notFound` after HTTP 404 |

`createOrUpdateContact` identifies a contact by email and sets the provided attributes to absolute values. Brevo's `updateEnabled` makes a retry update that same contact. A 201 response includes a new contact ID; a 204 update does not. It does not expose provider attributes in the result. `getContact` returns only the contact ID and email.

## Authorization

Sign in at [Brevo API keys](https://app.brevo.com/settings/keys/api), open **Settings > SMTP & API > API keys**, choose **Generate a new API key**, name it, and copy its secret string once. Enter the key into Dex Web Connections' secret `api_key` field. Blank means the connection cannot make requests. The key travels only in the `api-key` header. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank to use the displayed defaults. Override the URL only for a compatible gateway, the timeout for network latency, or the byte limit for a larger response.

## Failure behavior

| Condition | Route |
| --- | --- |
| Valid create or update, or valid contact lookup | `saved` or `found` |
| Missing contact | `notFound` |
| Definite provider rejection, including invalid credentials | `providerRejected` |
| Malformed or oversized response | `invalidResponse` |
| Invalid local input or missing credentials | `defect` |
| Transport failure, 408, 409, 429, or 5xx | Retry with Dex's default one-hour budget |

Only the happy branch is wired in the [contact sync example](go/examples/contact-sync); selecting an optional branch fails its Flow. No `uncertain` branch is needed because contact creation or update is repeatable by email.

## Mocks

The generated `brevomock` package has realistic defaults for both operations: `savedDefaultContact` creates contact ID 21 for `member@meridian.example`, and `foundDefaultContact` retrieves the same contact. The example's mock-backed real-Dex test checks one upsert call with that email.

## Sources and verification

- [Brevo create contact](https://developers.brevo.com/reference/create-contact) documents `POST /v3/contacts`, `api-key`, `updateEnabled`, and HTTP 201.
- [Brevo contact sync guide](https://developers.brevo.com/docs/synchronise-contact-lists) documents HTTP 204 for an update.
- [Brevo get contact](https://developers.brevo.com/reference/get-contact-info) documents the email path, HTTP 200, and 404.
- [Brevo API key authentication](https://developers.brevo.com/docs/api-key-authentication) documents key setup and the header.

Live Brevo authorization and real-provider calls have not been verified. The mock-backed real-Dex example is left to CI.
