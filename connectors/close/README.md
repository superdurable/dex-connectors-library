# Close connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/close/go@v0.2.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/close`.

`github.com/superdurable/dex-connectors-library/connectors/close/go` exposes Dex Query and Mutation Steps against Close's public REST API. Its API key is resolved from the named connection for each call and is never placed in a URL, receipt, or Flow state.

## Connection

In Dex Web Connections, create a `close` connection named `close`. Open [Settings > Developer > API Keys](https://app.close.com/settings/api/), create the token, and paste it into the secret `api_key` field. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

The Python package is [`dex-connector-close`](python/README.md), imported as
`dex_connectors.close`.

The TypeScript package is [`@superdurable/connector-close`](typescript/README.md),
imported with operation-specific `newListLeadsStep` and `newGetLeadStep` factories.

## Operations

| Operation | Official API reference |
| --- | --- |
| `listLeads` | [Close listLeads](https://developer.close.com/api/resources/leads/list) |
| `getLead` | [Close getLead](https://developer.close.com/api/resources/leads/get) |
| `createLead` (Go) | [Close create lead](https://developer.close.com/api/resources/leads/create) |
| `updateLead` (Go) | [Close update lead](https://developer.close.com/api/resources/leads/update) |
| `deleteLead` (Go) | [Close delete lead](https://developer.close.com/api/resources/leads/delete) |
| `listContacts` (Go) | [Close list contacts](https://developer.close.com/api/resources/contacts/list) |
| `getContact` (Go) | [Close get contact](https://developer.close.com/api/resources/contacts/get) |
| `createContact` (Go) | [Close create contact](https://developer.close.com/api/resources/contacts/create) |
| `updateContact` (Go) | [Close update contact](https://developer.close.com/api/resources/contacts/update) |
| `deleteContact` (Go) | [Close delete contact](https://developer.close.com/api/resources/contacts/delete) |

Each operation returns a bounded, typed response. A 404 on a single-resource read or update selects `notFound`; a 404 on delete confirms that the record is absent. Definitive 4xx select `providerRejected`; 429 and 503 Retry. Absolute updates and deletes are repeat safe. Malformed and oversized read or update JSON selects `invalidResponse`; an unusable create response selects `uncertain`. Invalid local input or missing credentials selects `defect` before a request. Provider error bodies are not copied into failures.

Lead and contact creation use the `limited` repeat tier and a default cap of three possible sends. If a response contains a created ID, a later attempt reads that resource before resending. When the outcome stays unknown at the cap, the operation selects `uncertain`; an application must reconcile possible duplicates. Close does not document server-side deduplication for these POST requests. Opportunities, activities, and webhook subscriptions remain outside this release.

## Mocks

The generated `closemock` package includes one default Meridian answer per operation and an invalid-key rejection for each. The defaults describe a consistent set of project, lead, sheet, or file records.

## Example

The runnable [example](go/examples/close-lookup/README.md) registers a Worker and a Flow using the static `close` connection. It reads leads, creates a sample contact on the selected lead, and lists its contacts. Each real run creates a new contact. CI runs its mock-backed real-Dex integration test; local unit tests verify deterministic mapping.

The [TypeScript lookup example](typescript/examples/close-lookup/README.md) lists
leads, fetches the first lead, and verifies both provider reads through a real
Dex Worker and Client against a local Close HTTP fixture.

## Live verification

The API endpoint, authentication, fields, and HTTP statuses above were checked against the linked official docs. Provider authorization, account permissions, and live responses have not been exercised.
