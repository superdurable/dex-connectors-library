# Outlook Mail read Flow

The Flow in `flow.ts` reads one message by immutable Microsoft Graph ID and keeps its result in a Dex Attribute. Its static `ConnectionName` is `outlook-mail`.

Install dependencies with `npm ci` in `connectors/microsoft/outlook-mail/typescript`. Start `dexcli dev` and create an Outlook Mail connection named `outlook-mail` in Dex Web following the [connector setup guide](../../../README.md). Run `npm run example:worker`, then `npm run example:start -- MESSAGE_ID` in a second terminal. The starter prints the Flow ID.

`npm run test:integration` launches its own real Dex development server on ports 40950 and 40951, starts a Worker with the same Flow and a scripted provider, and checks completion and optional branch failure. It does not need Microsoft credentials.

For a strict FDG 2.0 rendering from this checkout, build the package and link its manifest locally so Dex CLI can resolve the unpublished package name:

```sh
npm run build
mkdir -p node_modules/@superdurable
ln -s ../.. node_modules/@superdurable/connector-microsoft-outlook-mail
dexcli visualize examples/read-message/flow.ts --schema-version 2.0 --json --out /tmp/outlook-mail-flow
rm node_modules/@superdurable/connector-microsoft-outlook-mail
```

Run these commands from `connectors/microsoft/outlook-mail/typescript`. The generated graph has no diagnostics. The link lives only in ignored `node_modules`.
