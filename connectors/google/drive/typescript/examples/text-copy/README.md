# Google Drive text copy

The [Flow](flow.ts) searches for one exact source name, reads its bounded text, uploads a copy into My Drive, and reads the copy metadata. All four operations use `google-drive-files`; only their happy branches are wired. An ambiguous source or any optional connector branch fails the Flow.

Configure `google-drive / google-drive-files` in Dex Web **Connections** using the [connector authorization guide](../../README.md). The example has no operation ConfigurationUI fields and uploads into the My Drive root. The manifest supplies all fixed Google endpoints, scopes, and safety limits. The Worker resolves the connection once at startup, so restart it after changing connection settings.

Install this package, start `dexcli dev` with unused gRPC and Web ports, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, and run:

```bash
npm run example:worker
npm run example:start -- examples/text-copy/input.example.json
```

The starter's JSON fields are `sourceName` and `copyName`. The [integration test](integration.test.ts) runs this Flow through a real Dex Worker and Client with a local provider stand-in bound to a free port. Live provider authorization and behavior are unverified.
