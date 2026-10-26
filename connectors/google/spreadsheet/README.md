# Google Sheets Connector

The Google Sheets Connector exposes operation-specific Dex Step factories:

- `spreadsheet.NewGetValuesStep`
- `spreadsheet.NewFindRowStep`
- `spreadsheet.NewUpsertRowStep`

It uses a separate `google-sheets-oauth` Connection with the least-privilege
`drive.file` scope. A Google Picker grant selects each accessible spreadsheet;
manual IDs work only for files already granted to the application. The
generated `Credentials` keeps the OAuth client, access token, and refresh token
in the project connection that `NewProjectConnection` opens, as
[`examples/upsert-contact/main.go`](examples/upsert-contact/main.go) does. That
connection admits one refresh per credential generation across application
replicas and stores the complete replacement before the call uses it. Google
refresh-token rotation replaces the previous token only when Google returns a
new one, and terminal `invalid_grant` responses require reauthorization.

`UpsertRow` first reads the target sheet and matches a stable key column. It
updates one match, appends when absent, and returns the `conflict` branch for
duplicates. An ambiguous provider write returns `uncertain`; retrying the same
Step execution retains its Call ID and queries before writing again.

Every operation uses `defect` for invalid local input, connection configuration,
or Connector contract violations. A conclusive Google API refusal uses
`providerRejected`; malformed or oversized reads before a write use
`invalidResponse`. Safe query transport, rate-limit, and availability failures
retry instead of producing a branch.

The `ui/` package builds the credential-safe Studio setup bundle published as
`connector-ui.tgz` with the Connector release.

## Test with spreadsheetmock

The generated `spreadsheetmock` package answers every operation's happy path by
default, and the defaults describe one sheet: a `Contacts` tab with the header
`email`, `name`, `status`, Ada Lovelace (`ada@example.test`, active) on row 2
and Grace Hopper (`grace@example.test`, pending) on row 3. `getValues` reads
that range and `findRow` finds Ada on row 2, whatever range or key the request
names. `upsertRow` answers `updated` on row 2 (`Contacts!A2:C2`) for any input,
so a Flow that expects an `inserted` result, another row, or another sheet
needs a script. Pass the typed connection to the example Flow to test without
Google credentials or response scripts:

```go
mock := spreadsheetmock.New(t, ConnectionName)
flow := NewFlow(mock.Connection())
```

This snippet comes from
[`workflow_mock_integration_test.go`](examples/upsert-contact/flow/workflow_mock_integration_test.go).
Named cases cover missing ranges or rows, duplicate keys, and provider refusals.

## Example

[`examples/upsert-contact`](examples/upsert-contact) is a runnable Dex Web
**Start Flow** example that upserts one contact row by email address.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
npm ci --prefix ui
npm test --prefix ui
npm run build --prefix ui
```
