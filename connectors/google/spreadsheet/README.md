# Google Sheets Connector

The Google Sheets Connector exposes operation-specific Dex Step factories:

- `spreadsheet.NewGetValuesStep`
- `spreadsheet.NewFindRowStep`
- `spreadsheet.NewUpsertRowStep`

It uses a separate `google-sheets-oauth` Connection with the least-privilege
`drive.file` scope. A Google Picker grant selects each accessible spreadsheet;
manual IDs work only for files already granted to the application. The
generated `Credentials` contains a short-lived access token only. Refresh
tokens remain in the hosting application's OAuth broker.

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
