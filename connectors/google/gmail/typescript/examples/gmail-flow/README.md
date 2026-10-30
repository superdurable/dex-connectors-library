# Gmail TypeScript example

The three Flows each start with one connector operation Step: [read](read-flow.ts), [send](send-flow.ts), and [reply](reply-flow.ts). Their static `ConnectionName` is `gmail-inbox`; create that named Google Gmail connection in Dex Web and follow the authorization instructions in the [package README](../../README.md). For local development, point `DEX_CONNECTOR_CONFIG_FILE` at the private Dex Web `connections.json` file; never put its contents in Flow input or logs.

Start a local `dexcli dev` server, then run the Worker from `connectors/google/gmail/typescript`:

```sh
npm install
npm run example:worker
```

In another terminal, start a read with a Gmail provider message ID:

```sh
npm run example:start -- GMAIL_MESSAGE_ID
```

The [starter](start.ts) calls `Client.startFlow` and waits for terminal status. The [Worker](main.ts) registers all three Flows and resolves their shared connection at startup. Each Flow routes only its operation's happy branch; an unwired optional branch fails the Flow. To verify all three operation paths and retry behavior with a fake provider through a real Dex Server, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration` while `dexcli dev` is running.

Each local completion Step declares one `dex:group` and `dex:explanation` directive; connector Steps carry the same metadata in their static `annotations` object. Dex CLI v1.28.0 generated valid FDG 2.0 for all three source files with no diagnostics. From this repository checkout, build the package and make its generated `dex-connector.json` visible as an installed package before visualizing:

```sh
cd connectors/google/gmail/typescript
npm run build
mkdir -p examples/gmail-flow/node_modules/@superdurable /tmp/gmail-ts-fdg
ln -s ../../../.. examples/gmail-flow/node_modules/@superdurable/connector-google-gmail
cd examples/gmail-flow
for operation in read send reply; do
  dexcli visualize "${operation}-flow.ts" --language typescript --schema-version 2.0 --lint app --json --out "/tmp/gmail-ts-fdg/${operation}"
done
```

The `node_modules` link is local verification setup and is ignored by Git. Pass `/tmp/gmail-ts-fdg` to `dexcli dev --flow-rendering-dir` to inspect the three graphs in Dex Web. In an application that installs the published npm package, the link is unnecessary.
