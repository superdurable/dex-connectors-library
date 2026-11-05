# Smartsheet connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/smartsheet/go@v0.2.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/smartsheet`.

`github.com/superdurable/dex-connectors-library/connectors/smartsheet/go` exposes read-only Dex Query Steps against Smartsheet's public REST API. Its API key is resolved from the named connection for each call and is never placed in a URL, receipt, or Flow state.

## Connection

In Dex Web Connections, create a `smartsheet` connection named `smartsheet`. Open [Account icon > Personal Settings > API Access > Generate new access token](https://app.smartsheet.com), create the token, and paste it into the secret `api_key` field. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

The Python package is [`dex-connector-smartsheet`](python/README.md), imported as
`dex_connectors.smartsheet`.

## Operations

| Operation | Official API reference |
| --- | --- |
| `listSheets` | [Smartsheet listSheets](https://developers.smartsheet.com/api/smartsheet/openapi/sheets/list-sheets) |
| `getSheet` | [Smartsheet getSheet](https://developers.smartsheet.com/api/smartsheet/openapi/sheets/getsheet) |

Each operation returns a bounded, typed response. A 404 on a single-resource read selects `notFound`; other definitive 4xx select `providerRejected`; 408, 409, 429, 5xx, and interrupted reads Retry under the default one-hour budget. Malformed and oversized JSON selects `invalidResponse`. Invalid local input or missing credentials selects `defect` before a request. Provider error bodies are not copied into failures.

## Mocks

The generated `smartsheetmock` package includes one default Meridian answer per operation and an invalid-key rejection for each. The defaults describe a consistent set of project, lead, sheet, or file records.

## Example

The runnable [example](go/examples/smartsheet-lookup/README.md) registers a Worker and a Flow using the static `smartsheet` connection. CI runs its mock-backed real-Dex integration test; local unit tests verify deterministic mapping.

## Live verification

The API endpoint, authentication, fields, and HTTP statuses above were checked against the linked official docs. Provider authorization, account permissions, and live responses have not been exercised.

## TypeScript

The `typescript/` package publishes `@superdurable/connector-smartsheet` v0.1.0
with `listSheets` and `getSheet` Queries. Its runnable
[Smartsheet lookup example](typescript/examples/smartsheet-lookup/flow.ts)
uses the static `smartsheet` connection name and reads the first sheet returned
by a page listing. The package README describes local verification.
