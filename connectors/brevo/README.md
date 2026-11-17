# Brevo connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/brevo/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/brevo`.

Brevo contact and sender management plus attribute discovery for Dex. The module uses Brevo's public v3 REST API and a secret API key.

| Operation | Method | Result |
| --- | --- | --- |
| `createOrUpdateContact` | `POST /v3/contacts` with `updateEnabled: true` | `saved` after HTTP 201 or 204 |
| `getContact` | `GET /v3/contacts/{email}` | `found` after HTTP 200; `notFound` after HTTP 404 |
| `listContacts` | `GET /v3/contacts` | `listed` with a bounded page of IDs and emails |
| `updateContact` | `PUT /v3/contacts/{email}` | `updated` after HTTP 204 |
| `deleteContact` | `DELETE /v3/contacts/{email}` | `deleted` after HTTP 204 |
| `listAttributes` | `GET /v3/contacts/attributes` | `listed` with attribute names, categories, and types |
| `createAttribute` | `POST /v3/contacts/attributes/{category}/{name}` | `created` after HTTP 201 |
| `updateAttribute` | `PUT /v3/contacts/attributes/{category}/{name}` | `updated` after HTTP 204 |
| `deleteAttribute` | `DELETE /v3/contacts/attributes/{category}/{name}` | `deleted` after HTTP 204 |
| `listSenders` | `GET /v3/senders` | `listed` with sender IDs, addresses, names, and active status |
| `createSender` | `POST /v3/senders` | `created` with the new sender ID |
| `updateSender` | `PUT /v3/senders/{senderId}` | `updated` after HTTP 204 |
| `deleteSender` | `DELETE /v3/senders/{senderId}` | `deleted` after HTTP 204 |

`createOrUpdateContact` identifies a contact by email and sets the provided attributes to absolute values. Brevo's `updateEnabled` makes a retry update that same contact. A 201 response includes a new contact ID; a 204 update does not. It does not expose provider attributes in the result. `getContact` returns only the contact ID and email.

The new `updateContact` sets absolute uppercase named attributes. `deleteContact` removes a contact by email. These writes are repeat-safe; a retry of an update sets the same values and a retry of a deletion targets the same address. `listContacts` accepts a `limit` from 1 to 1000 and a zero-based `offset`; a zero limit uses 50. Email can be empty for phone-only contacts. The discovery Queries expose no contact attributes, sender IPs, or credentials.

`createSender` uses a bounded three-send policy because Brevo does not offer an idempotency key for this endpoint. An unknown result retries until the cap, then returns `uncertain`; Brevo may send a verification email to the new sender address. `updateSender` sets an absolute display name, and `deleteSender` removes the numeric sender ID. Those two writes are repeat-safe. The sender operations do not manage dedicated IP associations.

`createAttribute` accepts normal, transactional, category, calculated, and global categories. Normal and transactional attributes need a `type`; category attributes need `type: category` and an enumeration; calculated and global attributes need a formula in `value`. Normal `multiple-choice` attributes need `options`. Creation has the same three-send uncertainty cap as sender creation. `updateAttribute` replaces a normal multiple-choice option set, category enumeration, or calculated/global formula. `deleteAttribute` removes an attribute in any supported category. Attribute names are uppercase letters, digits, and underscores.

The Python package is [`dex-connector-brevo`](python/README.md), imported as
`dex_connectors.brevo`.

The TypeScript package is [`@superdurable/connector-brevo`](typescript/README.md).
Its runnable [contact sync Flow](typescript/examples/contact-sync) creates or updates
and then reads a contact through one static Dex connection.

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

Only the happy branches are wired in the [contact sync example](go/examples/contact-sync), which lists attribute definitions after saving a contact and optionally updates a sender and a normal multiple-choice attribute. Selecting an optional branch fails its Flow. No `uncertain` branch is needed for absolute updates and deletions.

## Mocks

The generated `brevomock` package has realistic defaults for both operations: `savedDefaultContact` creates contact ID 21 for `member@meridian.example`, and `foundDefaultContact` retrieves the same contact. The example's mock-backed real-Dex test checks one upsert call with that email.

## Sources and verification

- [Brevo create contact](https://developers.brevo.com/reference/create-contact) documents `POST /v3/contacts`, `api-key`, `updateEnabled`, and HTTP 201.
- [Brevo contact sync guide](https://developers.brevo.com/docs/synchronise-contact-lists) documents HTTP 204 for an update.
- [Brevo get contact](https://developers.brevo.com/reference/get-contact-info) documents the email path, HTTP 200, and 404.
- [Brevo list contacts](https://developers.brevo.com/reference/get-contacts), [update contact](https://developers.brevo.com/reference/update-contact), and [delete contact](https://developers.brevo.com/reference/delete-contact) document pagination and contact writes.
- [Brevo list attributes](https://developers.brevo.com/reference/get-attributes) and [list senders](https://developers.brevo.com/reference/get-senders) document discovery responses.
- [Brevo create attribute](https://developers.brevo.com/reference/create-attribute), [update attribute](https://developers.brevo.com/reference/update-attribute), and [delete attribute](https://developers.brevo.com/reference/delete-attribute) document category-specific fields and response codes.
- [Brevo create sender](https://developers.brevo.com/reference/create-sender), [update sender](https://developers.brevo.com/reference/update-sender), and [delete sender](https://developers.brevo.com/reference/delete-sender) document sender management and response codes.
- [Brevo API key authentication](https://developers.brevo.com/docs/api-key-authentication) documents key setup and the header.

Live Brevo authorization and real-provider calls have not been verified. The mock-backed real-Dex example is left to CI.
