# Harvest connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/harvest/go@v0.3.0` after this version is released.

Moved from `github.com/superdurable/dex-connectors-library/connectors/harvest`.

`github.com/superdurable/dex-connectors-library/connectors/harvest/go` exposes Dex Query and Mutation Steps against Harvest's public REST API. Its API key is resolved from the named connection for each call and is never placed in a URL, receipt, or Flow state.

## Connection

In Dex Web Connections, create a `harvest` connection named `harvest`. Open [Harvest ID > Developers > Personal Access Tokens](https://id.getharvest.com/developers), create the token, and paste it into the secret `api_key` field. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. Copy the decimal account ID shown with the personal token into non-secret accountId.

The Python package is [`dex-connector-harvest`](python/README.md), imported as
`dex_connectors.harvest`.

The TypeScript package is [`@superdurable/connector-harvest`](typescript/README.md), with a [runnable Harvest lookup Flow](typescript/examples/harvest-lookup/README.md).

## Operations

| Operation | Official API reference |
| --- | --- |
| `listProjects` | [Harvest listProjects](https://help.getharvest.com/api-v2/projects-api/projects/projects/) |
| `getProject`, `createProject`, `updateProject`, `deleteProject` | [Harvest Projects API](https://help.getharvest.com/api-v2/projects-api/projects/projects/) |
| `listTimeEntries` | [Harvest listTimeEntries](https://help.getharvest.com/api-v2/timesheets-api/timesheets/time-entries/) |
| `getTimeEntry`, `createTimeEntryByDuration`, `createTimeEntryByStartEnd`, `updateTimeEntry`, `deleteTimeEntry`, `deleteTimeEntryExternalReference`, `stopTimeEntry` | [Harvest Time Entries API](https://help.getharvest.com/api-v2/timesheets-api/timesheets/time-entries/) |
| `listUsers`, `getUser`, `getCurrentUser`, `createUser`, `updateUser`, `deleteUser` | [Harvest Users API](https://help.getharvest.com/api-v2/users-api/users/users/) |
| `listClients`, `getClient`, `createClient`, `updateClient`, `deleteClient` | [Harvest Clients API](https://help.getharvest.com/api-v2/clients-api/clients/clients/) |

Each operation returns a bounded, typed response. A 404 on a single-resource read selects `notFound`; other definitive 4xx select `providerRejected`; 408, 409, 429, 5xx, and interrupted reads Retry under the default one-hour budget. Malformed and oversized JSON selects `invalidResponse`. Invalid local input or missing credentials selects `defect` before a request. Provider error bodies are not copied into failures.

`createClient` is a limited-repeat Mutation with at most three sends because Harvest does not support an idempotency key and client names are not unique enough for reliable read-back. If a possible create remains unconfirmed at the cap, it selects `uncertain`; inspect Harvest before another create. `updateClient` applies absolute fields and `deleteClient` is idempotent, so unknown responses Retry safely. Each requires the connection owner's Harvest permission to manage clients.

`createProject` also uses a three-send limited tier because Harvest does not deduplicate project POSTs. It reads back a returned project ID on recovery; when no ID was received, a project name alone cannot safely identify the create. `updateProject` sends absolute fields and `deleteProject` uses repeat-safe DELETE. Project writes require Administrator or Manager access to managed projects.

Both completed time-entry create operations use the limited tier with at most three sends. Duration creation requires positive hours, and clock-time creation requires both start and end times, so these operations do not start a running timer. `updateTimeEntry`, both deletions, and `stopTimeEntry` use the safe tier. A repeated stop reads the entry first and confirms a stopped timer without issuing another PATCH.

`createUser` sends a Harvest invitation email and uses the limited tier with at most three sends. A possible earlier invitation without a returned user ID cannot be confirmed by the create response alone; inspect Harvest Users before inviting again after `uncertain`. User updates set absolute fields, and user deletion is repeat-safe. Administrator permissions are generally required for creation and deletion; `getCurrentUser` works with the connected user's own access.

## Mocks

The generated `harvestmock` package includes one default Meridian answer per operation and an invalid-key rejection for each. The defaults describe a consistent set of project, lead, sheet, or file records.

## Example

The runnable [example](go/examples/harvest-lookup/README.md) registers a Worker and a Flow using the static `harvest` connection. It lists projects, reads the first project's details, lists time entries, reads the first time-entry detail, reads the connected user, and lists clients. CI runs its mock-backed real-Dex integration test; local unit tests verify deterministic mapping.

## Live verification

The API endpoint, authentication, fields, and HTTP statuses above were checked against the linked official docs. Provider authorization, account permissions, and live responses have not been exercised.
