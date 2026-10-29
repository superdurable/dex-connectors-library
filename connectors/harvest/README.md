# Harvest connector

`github.com/superdurable/dex-connectors-library/connectors/harvest` exposes read-only Dex Query Steps against Harvest's public REST API. Its API key is resolved from the named connection for each call and is never placed in a URL, receipt, or Flow state.

## Connection

In Dex Web Connections, create a `harvest` connection named `harvest`. Open [Harvest ID > Developers > Personal Access Tokens](https://id.getharvest.com/developers), create the token, and paste it into the secret `api_key` field. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. Copy the decimal account ID shown with the personal token into non-secret accountId.

## Operations

| Operation | Official API reference |
| --- | --- |
| `listProjects` | [Harvest listProjects](https://help.getharvest.com/api-v2/projects-api/projects/projects/) |
| `listTimeEntries` | [Harvest listTimeEntries](https://help.getharvest.com/api-v2/timesheets-api/timesheets/time-entries/) |

Each operation returns a bounded, typed response. A 404 on a single-resource read selects `notFound`; other definitive 4xx select `providerRejected`; 408, 409, 429, 5xx, and interrupted reads Retry under the default one-hour budget. Malformed and oversized JSON selects `invalidResponse`. Invalid local input or missing credentials selects `defect` before a request. Provider error bodies are not copied into failures.

## Mocks

The generated `harvestmock` package includes one default Meridian answer per operation and an invalid-key rejection for each. The defaults describe a consistent set of project, lead, sheet, or file records.

## Example

The runnable [example](examples/harvest-lookup/README.md) registers a Worker and a Flow using the static `harvest` connection. CI runs its mock-backed real-Dex integration test; local unit tests verify deterministic mapping.

## Live verification

The API endpoint, authentication, fields, and HTTP statuses above were checked against the linked official docs. Provider authorization, account permissions, and live responses have not been exercised.
