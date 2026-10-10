# Google Sheets Connector

The Google Sheets Connector exposes operation-specific Dex Step factories:

- `spreadsheet.NewGetValuesStep`
- `spreadsheet.NewFindRowStep`
- `spreadsheet.NewUpsertRowStep`

## Authorization

- `google-oauth` is the default and keeps the least-privilege `drive.file`
  scope. A Google Picker grant selects each accessible spreadsheet; manual IDs
  work only for files already granted to the application. Connections saved
  before this connector declared authorization methods carry no `auth_method`
  and keep using this method.
- `workspace-domain-delegation` is an administrator-only option. It signs an
  RS256 service-account assertion for the configured Workspace user and mints a
  delegated access token. The administrator authorizes the service account
  client ID for `https://www.googleapis.com/auth/spreadsheets` and
  `https://www.googleapis.com/auth/drive.readonly`, keeping any scopes the same
  client already has. Every spreadsheet the delegated user can open is then
  reachable without a Picker grant.

`CredentialRefreshDriver` delegates both token exchanges to
`sdkgo/oauthtoken`: the refresh-token grant with `ClientSecretPost`, and the
JWT-bearer grant with `SignJWTBearerAssertion`. `invalid_grant`,
`invalid_client`, `unauthorized_client`, an invalid service-account key or
delegated user, and a returned scope list without the method's scopes require
reauthorization. Google refresh-token rotation replaces the previous token only
when Google returns a new one.

The project connection that `NewProjectConnection` opens, as
[`examples/upsert-contact/main.go`](examples/upsert-contact/main.go) does,
admits one refresh per credential generation across application replicas and
stores the complete replacement before the call uses it. Refresh tokens, client
secrets, and service-account keys stay in encrypted project storage and never
enter a Flow.

## Operations

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
