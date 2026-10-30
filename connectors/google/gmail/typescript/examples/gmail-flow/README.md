# Gmail TypeScript example

This Flow routes `read`, `send`, and `reply` through the connector's operation-specific Dex Steps. `ConnectionName` in [`flow.ts`](flow.ts) is `gmail-inbox`; create that named Google Gmail connection in Dex Web and follow the authorization instructions in the [package README](../../README.md). For local development, point `DEX_CONNECTOR_CONFIG_FILE` at the private Dex Web `connections.json` file; never put its contents in Flow input or logs.

Start a local `dexcli dev` server, then run the Worker from `connectors/google/gmail/typescript`:

```sh
npm install
npm run example:worker
```

In another terminal, start a read with a Gmail provider message ID:

```sh
npm run example:start -- GMAIL_MESSAGE_ID
```

The [starter](start.ts) calls `Client.startFlow` and waits for terminal status. The [Worker](main.ts) registers the same Flow and resolves its connection at startup. The Flow routes only each operation's happy branch; an unwired optional branch fails the Flow. To verify all three operation paths and retry behavior with a fake provider through a real Dex Server, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration` while `dexcli dev` is running.

The source contains one `dex:group` and `dex:explanation` directive for every Step type. Dex CLI currently visualizes Go or Python sources, and Dex Web TypeScript FDG 2.0 identity support (R4) is pending; no TypeScript graph generation command or rendered graph is available yet.
