# Google Sheets connector for TypeScript

This package binds `getValues`, `findRow`, and `upsertRow` to Dex Steps. The
`google-sheets-contacts` example in `examples/upsert-contact/` is a runnable
Flow that reads a stable contact key and replaces or appends a row. The upsert
uses a durable single dispatch fence before an append and confirms uncertain
responses by reading the key again.

Use `newProjectConnection(project, ConnectionName)` in a Worker after loading
Dex project configuration. The generated OAuth refresh driver exchanges stored
Google refresh tokens, checks the `drive.file` scope, and persists refreshed
credentials through the host. Keep credentials in Dex Web Connections, never
in Flow input or persisted Attributes.

Run `npm run check` for the package suite and
`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dex-port> npm run test:integration` against
`dexcli dev`. The integration test starts a local Sheets stand-in on an OS
assigned port and exercises the example through a real Dex Worker and Client.
Live Google OAuth consent, spreadsheet selection, and API writes are unverified.
