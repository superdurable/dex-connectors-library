# Microsoft Excel Connector

> **Verification status: partial live.** The example ran on a real Dex stack,
> through Dex Web Start Flow and 13 real-Dex tests, against a local stand-in for
> Microsoft Graph. Only invalid-credential probes reached Microsoft. No live
> workbook was read or written. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Microsoft Excel Connector reads and writes Excel workbooks stored in
OneDrive for work or school and in SharePoint document libraries, through the
Microsoft Graph workbook API at `https://graph.microsoft.com/v1.0`. It exposes
these operation-specific Dex Step factories:

| Operation | Kind | Happy branch | Other branches |
| --- | --- | --- | --- |
| `excel.NewGetTableRowsStep` | Query | `read` | `notFound`, `tooLarge`, `providerRejected`, `invalidResponse`, `defect` |
| `excel.NewGetValuesStep` | Query | `read` | `notFound`, `tooLarge`, `providerRejected`, `invalidResponse`, `defect` |
| `excel.NewUpdateValuesStep` | Mutation | `updated` | `notFound`, `providerRejected`, `defect` |
| `excel.NewAppendTableRowsStep` | Mutation | `appended` | `notFound`, `columnsMismatch`, `providerRejected`, `invalidResponse`, `uncertain`, `defect` |

- `getTableRows` reads an Excel table as rows keyed by column name, without its
  header and total rows. It is the "policy table" read: one Step returns every
  row, such as `{"Category":"travel","MaxAutoApproveUsd":500}`.
- `getValues` reads one bounded A1 range, such as `A2:E2`, as raw values and
  displayed text.
- `updateValues` writes literal values to one bounded A1 range whose size
  matches the values exactly.
- `appendTableRows` appends rows to a table, skipping every row whose key
  column value the table already holds.

Only the happy branch of each operation is required; an unwired optional branch
fails the Flow. The three idempotent operations use async Execute durability, a
30-second Execute timeout, and retry for up to two minutes. `appendTableRows`
uses sync durability and retries for up to five minutes; see
[Duplicate dispatch](#duplicate-dispatch).

The release has three Studio pickers, `workbookPicker`, `worksheetPicker`, and
`tablePicker`, and no Triggers; see [Not in this release](#not-in-this-release).

## Microsoft setup

A connection authorizes one Microsoft Entra work or school account with
delegated OAuth. Every request acts with that person's access, so a Flow can
use exactly the workbooks the person can open in OneDrive, SharePoint, or Teams.

**Microsoft OAuth needs Dex CLI 1.4.1 or later.** Microsoft does not echo
`offline_access` in the token response's `scope`, and Dex Web `cli-v1.4.0` and
earlier match every requested scope literally, so they reject the consent with
`CONNECTOR_OAUTH_SCOPE_INSUFFICIENT`. Dex Web `cli-v1.4.1` (dex#584) accepts a
returned refresh token as proof of `offline_access`.

1. Sign in to the Microsoft Entra admin center at `https://entra.microsoft.com`
   as at least an Application Developer, open **Entra ID > App registrations**,
   and choose **New registration**.
2. Under **Supported account types**, choose **Multiple Entra ID tenants**. The
   manifest's OAuth endpoints are static, so Dex uses
   `https://login.microsoftonline.com/organizations/oauth2/v2.0/authorize` and
   `.../token`; a single-tenant registration fails there with `AADSTS50194`.
   Personal Microsoft accounts are out of scope, because Excel's Graph API
   does not serve workbooks in consumer OneDrive.
3. Open **Authentication > Add Redirect URI**, choose **Web**, and add the exact
   Redirect URI Dex Web shows. Entra accepts `http` only for `localhost`
   redirect URIs, so open Dex Web at `http://localhost:<port>` rather than
   `http://127.0.0.1:<port>`.
4. Open **API permissions > Add a permission > Microsoft Graph > Delegated
   permissions** and add `Files.ReadWrite.All` and `offline_access`.
   `Files.ReadWrite.All` reads and writes every file the signed-in person can
   access; the narrower `Files.ReadWrite` covers only the person's own
   OneDrive, so it cannot reach SharePoint or shared workbooks. Neither needs
   administrator consent by default, but a tenant that blocks user consent
   needs an administrator to choose **Grant admin consent**.
5. Open **Certificates & secrets > Client secrets > New client secret** and
   copy the secret's **Value**, shown once. Copy the **Application (client)
   ID** from **Overview**. Enter both in Dex Web, choose **Authorize**, and
   sign in.

Dex requests exactly `offline_access Files.ReadWrite.All`, with PKCE. The
connector refreshes the one-hour access token at the organizations token
endpoint with the client ID and secret in the form body, requests the same two
scopes again, and stores the refresh token Microsoft returns each time. A
returned scope without `Files.ReadWrite.All`, compared without case and with or
without the `https://graph.microsoft.com/` prefix, requires reauthorization.
`invalid_grant`, `invalid_client`, `unauthorized_client`,
`interaction_required`, `consent_required`, `invalid_scope`,
`invalid_request`, `unsupported_grant_type`, and `invalid_resource` also
require reauthorization; every other token failure, including a 5xx, is
retried. Client secrets expire on the date chosen when they are created.

### Why there is no app-only method

Microsoft's permission table for every workbook API this connector calls,
including table rows, table add, range get and update, and the worksheet and
table lists, says **Application: Not supported** in both v1.0 and beta, and
names `Files.ReadWrite` as the least privileged delegated permission. A
client-credentials method would therefore depend on undocumented behavior, so
this release ships delegated OAuth only. The manifest already declares
`auth.methods`, so adding an app-only method later does not change the stored
connection shape.

## Workbooks, worksheets, and tables

Every operation addresses a workbook by its Microsoft Graph drive ID and drive
item ID, `/drives/{driveId}/items/{workbookId}/workbook`, which works for
OneDrive for work or school and for every SharePoint or Teams document library.
Only `.xlsx` and `.xlsm` workbooks work; Microsoft's Excel API rejects `.xls`.
A worksheet or table is given by its stable ID, such as
`{00000000-0001-0000-0000-000000000000}`, which survives a rename, or by its
name. Addresses are one cell or rectangle in A1 form, such as `A2:E2`, without
a worksheet name, `$`, or whole columns and rows, which Excel cannot write.

The Studio units derive these values instead of asking for them:

- `workbookPicker` searches the signed-in person's OneDrive and the files
  shared with them (`GET /me/drive/search(q='.xlsx')`, which returns shared
  items with their own drive in `remoteItem`), or resolves a pasted OneDrive
  or SharePoint sharing link through `GET /shares/u!{token}/driveItem`. It
  stores `driveId`, `workbookId`, and `workbookName`. Manual drive and item ID
  entry stays available.
- `worksheetPicker` and `tablePicker` list the chosen workbook's worksheets and
  tables and store the picked item's ID and name, or a typed name.

Dex Web's Studio commands accept only the characters `A-Z a-z 0-9 . _ ~ -` in a
path parameter, and OneDrive for work or school and SharePoint drive IDs start
with `b!`. The worksheet and table commands therefore carry the `b!` literally,
`/drives/b!{driveKey}/...`, and the bundle passes the rest of the drive ID.
For a drive ID without that prefix the two pickers offer typed names only.

## Sessions

The connector sends no `workbook-session-id`, so every request is sessionless
and Excel persists each write when it answers. Microsoft recommends a session
"if your application needs to make more than one or two calls"; each operation
here makes one to three, a session would add `createSession` and
`closeSession`, a persistent session expires after about five minutes without
use, and `createSession` can take Microsoft's long-running `202 Accepted`
pattern with polling every 30 seconds, which does not fit a 30-second Execute.
A session could not be kept across Steps or Dex retries anyway, because they
may run on another Worker.

## Cell values

`excel.CellValue` is Graph's raw cell value: text, a number, a Boolean, or empty
text for an empty cell. A cell with an error reads as its error text, such as
`#N/A`, and a date reads as its serial number; `getValues` also returns the
displayed text, such as `10/1/2026`. Results encode values as plain JSON.

Every write is literal. Microsoft documents that Excel reads `values` like
typed input, turning `=`, `+`, or `-` text into formulas and parsing numbers
and dates, so the connector sends text with Excel's leading apostrophe text
prefix. `TextCellValue("00123")` stays the text `00123`, and
`TextCellValue("=HYPERLINK(...)")` never becomes a formula. Build values with
`TextCellValue`, `NumberCellValue`, `BooleanCellValue`, and `EmptyCellValue`;
a zero `CellValue` is rejected as `defect`, because Graph treats `null` as
"leave this cell unchanged". Formulas are not written in this release.

## Duplicate dispatch

Microsoft Graph has no idempotency key for workbook writes, and Dex re-dispatches
an async Step whose local attempt passes about seven seconds.

`updateValues` writes absolute values, so a repeated dispatch writes the same
content again and the range ends the same. A dispatch that answers late can
still land after a later Step's write, so write one range from one Step per
Flow, or read it back first, as the example does.

`appendTableRows` makes three requests: the table's columns, the key column's
data cells (`/columns/{id}/dataBodyRange`), and one `rows/add` with only the
rows whose key is missing, ordered by column and with omitted columns empty.
Reading before writing alone is not enough, because Excel queues concurrent
writes to a workbook: a second dispatch could read the key column while the
first append is still queued. So the operation:

- runs with **sync** durability, so Dex never dispatches it again while an
  attempt is in flight;
- records a Dex heartbeat checkpoint before it sends `rows/add`. A later
  attempt of the same Step execution that finds the checkpoint never sends
  again: it selects `appended` with `isFromEarlierAttempt` when the key column
  holds every key, and `uncertain` otherwise;
- clears the checkpoint only for a response that proves Excel appended
  nothing: any 4xx except 408, plus 501 and 503;
- treats a transport failure, 408, 500, 502, and 504 as unknown and retries
  after 10 seconds, so the next attempt can find the rows in the key column.

Microsoft's guide says to repeat an append that answered 504. The connector
reads the key column instead, so a 504 whose append landed converges to
`appended`, and one that did not land selects `uncertain` instead of risking a
second row. Wire `uncertain` to a person, or to a Timer and a later
`getTableRows`, as the example's `ReportExcelDecisionUncertain` Step does. A
key the table already holds is never appended again, by any Flow; key text
stored with a kept apostrophe still matches.

## Errors and throttling

Errors follow Microsoft's Excel error handling guide: the first known
second-level `innerError.code` wins, then the top-level `error.code`, then the
HTTP status. Codes are compared without case, and no `error.message` reaches a
Failure.

| Graph response | Reads | `updateValues` |
| --- | --- | --- |
| 429, 503, `tooManyRequestsUncategorized`, `serviceUnavailableUncategorized`, `transientFailure` | Retry after `Retry-After` | Retry after `Retry-After` |
| 502, 504, 408, `gatewayTimeoutUncategorized`, 500 without a known code | Retry | Retry |
| 409 `accessConflict` (another client locked the workbook) | Retry | Retry |
| 404, `itemNotFound`, `notFoundUncategorized` | `notFound` | `notFound` |
| 413, `payloadTooLargeUncategorized`, `rangeExceedsLimit`, or a response above `maxResponseBytes` | `tooLarge` | `providerRejected` |
| 500 `internalServerErrorUncategorized`, `generalException`, `unsupportedWorkbook`, other 4xx and 501 | `providerRejected` | `providerRejected` |

After a 401 the connector asks once for a token refresh, which the project
connection performs only when the stored expiry has passed, and then resends
once; otherwise the 401 selects `providerRejected`. Microsoft documents
Excel throttling limits of 5,000 requests per 10 seconds per app and 1,500 per
app per tenant, and recommends sending one request at a time per workbook.
Dex's retry policy and `Retry-After` handle throttling; a Flow that writes one
workbook from parallel Steps should serialize those Steps.

## Project connection

Dex Web saves one connection per name in encrypted project storage. Its private
credential holds `auth_method: microsoft-oauth`, the client ID and secret, and
the `access_token` and `refresh_token` from consent. Load the project
configuration once at application startup and open the connection by the name
its operations use, as
[`examples/approval-decision/main.go`](examples/approval-decision/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := excel.NewProjectConnection(project, approvaldecision.ConnectionName, connectionOptions()...)
```

`projectconfig.LoadFromEnvironment` reads the `DEX_PROJECT_*` configuration
that Dex Web or Superverse Studio writes; see
[`sdkgo/projectconfig`](../../../sdkgo/projectconfig/README.md#application-loading).
Set the same `ConnectionName` beside the typed `Connection` in each operation:
a Step whose `ConnectionName` is empty or differs from its connection's name
panics at construction.

Credentials are reread before every provider call, so reauthorization needs no
restart. The cell and response limits are startup configuration:
`maxResponseBytes` bounds one Graph response, and `maxCells`, at most 100,000,
bounds the cells one operation reads or writes.

## Example

[`examples/approval-decision`](examples/approval-decision/README.md) uses every
operation in one Flow started from Dex Web: it reads an approval-policy table,
decides a spending request, appends the decision to a decision log keyed by
request ID, writes the latest decision to a summary range, and reads it back.

## Not in this release

- **Triggers.** A change Trigger would need either Graph change notifications,
  which require answering the `validationToken` handshake that
  `sdkgo/webhooktrigger` does not support yet plus subscription renewal, or
  delta-query polling of the drive with a durable per-binding cursor, which
  `sdkgo` does not provide. Neither notifies about individual cells or rows,
  so a Trigger would also need a table read after each change.
- **App-only access**, because Microsoft documents no application permission
  for the workbook API; see [Why there is no app-only method](#why-there-is-no-app-only-method).
- Workbook sessions, formulas, formatting, charts, and worksheet or table
  creation.
- Personal Microsoft accounts and national clouds, whose sign-in hosts differ
  from the manifest's static endpoints.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go test -tags=integration ./examples/approval-decision/... -count=1 -v
(cd ui && npm ci && npm test && npm run build)
```

The integration tests need a Dex Server at `DEX_FLOW_SERVICE_ADDRESS`; they
run the example against `internal/fakeexcel`, a stateful, credential-checking
Graph stand-in that stores typed input as Excel does.

Unverified live behavior:

- every Graph workbook response against a real OneDrive or SharePoint
  workbook, including `$select` on table columns and ranges, the shape of a
  `rows/add` response for several rows, and whether a sessionless read
  immediately reflects the previous sessionless write;
- Excel's handling of the apostrophe text prefix written through Graph
  `values`, which the connector relies on to keep text literal;
- the Microsoft OAuth consent, code exchange, and refresh with a real app
  registration, including the returned `scope` string;
- the picker commands against real Graph: `/me/drive/search(q='.xlsx')`, the
  `u!` sharing-token path, and the literal `b!` prefix in the worksheet and
  table paths; with a placeholder token they reached Dex Web's broker and
  failed cleanly;
- application permissions, which Microsoft documents as unsupported.
