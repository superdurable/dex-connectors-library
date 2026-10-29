# Close connector

`github.com/superdurable/dex-connectors-library/connectors/close` exposes read-only Dex Query Steps against Close's public REST API. Its API key is resolved from the named connection for each call and is never placed in a URL, receipt, or Flow state.

## Connection

In Dex Web Connections, create a `close` connection named `close`. Open [Settings > Developer > API Keys](https://app.close.com/settings/api/), create the token, and paste it into the secret `api_key` field. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

## Operations

| Operation | Official API reference |
| --- | --- |
| `listLeads` | [Close listLeads](https://developer.close.com/api/resources/leads/list) |
| `getLead` | [Close getLead](https://developer.close.com/api/resources/leads/get) |

Each operation returns a bounded, typed response. A 404 on a single-resource read selects `notFound`; other definitive 4xx select `providerRejected`; 408, 409, 429, 5xx, and interrupted reads Retry under the default one-hour budget. Malformed and oversized JSON selects `invalidResponse`. Invalid local input or missing credentials selects `defect` before a request. Provider error bodies are not copied into failures.

## Mocks

The generated `closemock` package includes one default Meridian answer per operation and an invalid-key rejection for each. The defaults describe a consistent set of project, lead, sheet, or file records.

## Example

The runnable [example](examples/close-lookup/README.md) registers a Worker and a Flow using the static `close` connection. CI runs its mock-backed real-Dex integration test; local unit tests verify deterministic mapping.

## Live verification

The API endpoint, authentication, fields, and HTTP statuses above were checked against the linked official docs. Provider authorization, account permissions, and live responses have not been exercised.
