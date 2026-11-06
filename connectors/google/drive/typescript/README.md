# Google Drive TypeScript connector

`@superdurable/connector-google-drive` binds Google Drive search, metadata, bounded text, and upload operations to Dex Steps. The package version is `0.1.0`; the connector manifest remains at its independent Go release version.

The [text copy Flow](examples/text-copy/flow.ts) uses all four operation factories with the static `google-drive-files` connection. The [Worker](examples/text-copy/main.ts) loads the project connection at startup, and the [starter](examples/text-copy/start.ts) reads a JSON input. Optional branches are unwired, so a missing source, unsupported content, provider rejection, uncertain upload, or invalid response fails the Flow.

```bash
npm ci
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:42050 npm run test:integration
```

Start `dexcli dev -dex-port 42050 -web-port 42051 -open=false` before the integration command. Choose unused ports when running your own stack. The test starts a provider stand-in on an OS-assigned free port and runs the checked-in Flow through a real Dex Worker and Client.

For an application, configure the `google-drive / google-drive-files` connection in Dex Web. The manifest's Google OAuth method requests `drive.readonly` and `drive.file`; Workspace domain delegation also requires a service-account JSON key and one delegated user. The generated credential decoders and refresh declaration keep tokens, client secrets, and service-account keys in project connection storage outside Flow values. The TypeScript client uses the generated refresh declaration for both methods.

The package's provider tests replay the [Go-recorded scenarios](../../../../conformance/connectors/google/drive/scenarios.json). The Dex integration test uses a local provider stand-in; live Google OAuth, domain delegation, and Drive API behavior remain unverified.
