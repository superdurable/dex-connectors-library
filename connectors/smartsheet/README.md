# Smartsheet connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/smartsheet/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/smartsheet`.

`github.com/superdurable/dex-connectors-library/connectors/smartsheet/go` exposes Dex Query and Mutation Steps against Smartsheet's public REST API. Its API key is resolved from the named connection for each call and is never placed in a URL, receipt, or Flow state.

## Connection

In Dex Web Connections, create a `smartsheet` connection named `smartsheet`. Open [Account icon > Personal Settings > API Access > Generate new access token](https://app.smartsheet.com), create the token, and paste it into the secret `api_key` field. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

The Python package is [`dex-connector-smartsheet`](python/README.md), imported as
`dex_connectors.smartsheet`.

## Operations

| Operation | Official API reference |
| --- | --- |
| `listSheets` | [Smartsheet listSheets](https://developers.smartsheet.com/api/smartsheet/openapi/sheets/list-sheets) |
| `getSheet` | [Smartsheet getSheet](https://developers.smartsheet.com/api/smartsheet/openapi/sheets/getsheet) |
| `getRow` | [Smartsheet get row](https://developers.smartsheet.com/api/smartsheet/openapi/rows/get-row) |
| `addRow` | [Smartsheet add rows](https://developers.smartsheet.com/api/smartsheet/openapi/rows/rows-addtosheet) |
| `updateRow` | [Smartsheet update rows](https://developers.smartsheet.com/api/smartsheet/openapi/rows/update-rows) |
| `deleteRow` | [Smartsheet delete rows](https://developers.smartsheet.com/api/smartsheet/openapi/rows/delete-rows) |
| `createSheetInWorkspace` | [Create sheet in workspace](https://developers.smartsheet.com/api/smartsheet/openapi/sheets/create-sheet-in-workspace) |
| `createSheetInFolder` | [Create sheet in folder](https://developers.smartsheet.com/api/smartsheet/openapi/sheets/create-sheet-in-folder) |
| `updateSheet` | [Update sheet](https://developers.smartsheet.com/api/smartsheet/openapi/sheets/update-sheet) |
| `deleteSheet` | [Delete sheet](https://developers.smartsheet.com/api/smartsheet/openapi/sheets/delete-sheet) |
| `listColumns` | [List columns](https://developers.smartsheet.com/api/smartsheet/openapi/columns/columns-listonsheet) |
| `getColumn` | [Get column](https://developers.smartsheet.com/api/smartsheet/openapi/columns/column-get) |
| `addColumn` | [Add columns](https://developers.smartsheet.com/api/smartsheet/openapi/columns/columns-addtosheet) |
| `updateColumn` | [Update column](https://developers.smartsheet.com/api/smartsheet/openapi/columns/column-updatecolumn) |
| `deleteColumn` | [Delete column](https://developers.smartsheet.com/api/smartsheet/openapi/columns/column-delete) |
| `getFolder` | [Get folder metadata](https://developers.smartsheet.com/api/smartsheet/openapi/folders/get-folder-metadata) |
| `createFolderInWorkspace` | [Create folder in workspace](https://developers.smartsheet.com/api/smartsheet/openapi/workspaces/create-workspace-folder) |
| `createFolderInFolder` | [Create nested folder](https://developers.smartsheet.com/api/smartsheet/openapi/folders/create-folder-folder) |
| `updateFolder` | [Update folder](https://developers.smartsheet.com/api/smartsheet/openapi/folders/update-folder) |
| `deleteFolder` | [Delete folder](https://developers.smartsheet.com/api/smartsheet/openapi/folders/delete-folder) |
| `listWorkspaces` | [List workspaces](https://developers.smartsheet.com/api/smartsheet/openapi/workspaces/list-workspaces) |
| `getWorkspace` | [Get workspace metadata](https://developers.smartsheet.com/api/smartsheet/openapi/workspaces/get-workspace-metadata) |
| `createWorkspace` | [Create workspace](https://developers.smartsheet.com/api/smartsheet/openapi/workspaces/create-workspace) |
| `updateWorkspace` | [Update workspace](https://developers.smartsheet.com/api/smartsheet/openapi/workspaces/update-workspace) |
| `deleteWorkspace` | [Delete workspace](https://developers.smartsheet.com/api/smartsheet/openapi/workspaces/delete-workspace) |

Each operation returns a bounded, typed response. `addRow` accepts text cells and limits possibly effective sends to three; a fully exhausted cap selects `uncertain`. Absolute `updateRow` and `deleteRow` can safely retry. A 404 on a single-resource read selects `notFound`; other definitive 4xx select `providerRejected`; 408, 409, 429, 5xx, and interrupted reads Retry under the default one-hour budget. Malformed and oversized JSON selects `invalidResponse` for reads and safe writes. Invalid local input or missing credentials selects `defect` before a request. Provider error bodies are not copied into failures.

Sheet creation requires a parent workspace or folder and a primary text column. Both create operations allow at most three possibly effective sends; an unconfirmed result after the cap selects `uncertain`. Renaming and deletion are safe to retry. The connection token must have the provider's required permissions.
Column creation adds one `TEXT_NUMBER` column at a zero-based index and uses the same three-send cap; read, absolute rename, and delete are safe to retry. More specialized column types and conversions remain outside this typed operation.
Folder creation can target a workspace or parent folder; both creates limit possibly effective sends to three. Folder metadata reads, absolute rename, and deletion are safe to retry. Folder names may contain at most 50 characters.
Workspace listing uses provider token pagination with `maxItems` from 100 to 1000 and returns `lastKey` for the next page. Workspace creation limits possibly effective sends to three; metadata reads, absolute rename, and deletion are safe to retry.

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
