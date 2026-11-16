# Google Drive TypeScript connector

`@superdurable/connector-google-drive` binds Google Drive search, metadata, bounded text, and upload operations to Dex Steps, and provides the `fileCreated`, `fileUpdated`, and `fileTrashed` Triggers. The package version is `0.2.0`; the connector manifest remains at its independent Go release version.

The [text copy Flow](examples/text-copy/flow.ts) uses all four operation factories with the static `google-drive-files` connection. The [Worker](examples/text-copy/main.ts) loads the project connection at startup, and the [starter](examples/text-copy/start.ts) reads a JSON input. Optional branches are unwired, so a missing source, unsupported content, provider rejection, uncertain upload, or invalid response fails the Flow.

```bash
npm ci
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:42050 npm run test:integration
```

Start `dexcli dev -dex-port 42050 -web-port 42051 -open=false` before the integration command. Choose unused ports when running your own stack. The test starts a provider stand-in on an OS-assigned free port and runs the checked-in Flow through a real Dex Worker and Client.

## File Triggers

The three Triggers share one durable `changes.list` poller per connection, at behavior parity with the Go connector's [File Triggers](../README.md#file-triggers): the same classification, event IDs, folder and MIME type filters, rate-limit pauses, and page-token reseeds. `newProjectFileTriggerRunner` opens a project connection, decodes each selected binding's saved `FileTriggerConfiguration` with Go's strict JSON rules, and records every event in the binding's project Trigger inbox before the shared `poll-feeds/google-drive/<connection>/fileChanges/head` cursor commits. The cursor document and `FileEvent` inbox payloads are the Go connector's, so Go, Python, and TypeScript replicas can share them. `FileEvent` times are Go RFC 3339 strings that keep Drive's offset and nanoseconds. The connection settings `pollInterval` and `pollPageSize` (1 to 500) tune the poll, and the `triggerLogger` client option receives the runner's and inboxes' records, which never carry file names, page tokens, or credentials.

- `newFileTriggerRunner` takes an explicit `CursorStore`, such as `newMemoryCursorStore()`, for tests and applications without project storage.
- `newFileCreatedTrigger`, `newFileUpdatedTrigger`, and `newFileTrashedTrigger` poll one binding with an in-memory cursor, as Go's generated per-Trigger factories do; a restart reseeds.
- `defineFileCreatedTriggerBinding` and its siblings declare a Flow's static bindings and their `folderPicker` and `mimeTypePicker` units.
- `run` rejects with `TriggerDeliveryAbortedError` after its signal aborts.

The [file intake Flow](examples/file-intake/README.md) starts one Flow per created file; [`app.ts`](examples/file-intake/app.ts) wires it:

```ts
return newProjectFileTriggerRunner(project, ConnectionName, { fileCreatedRoutes: [{ bindingName: NewFileTriggerBinding, target }] }, options);
```

The Triggers require `@superdurable/connectors-sdk` 0.12.0, which provides the poll Trigger kit, and `@superdurable/dex` 1.45.4 or newer.

For an application, configure the `google-drive / google-drive-files` connection in Dex Web. The manifest's Google OAuth method requests `drive.readonly` and `drive.file`; Workspace domain delegation also requires a service-account JSON key and one delegated user. The generated credential decoders and refresh declaration keep tokens, client secrets, and service-account keys in project connection storage outside Flow values. The TypeScript client uses the generated refresh declaration for both methods.

The package's provider tests replay the [Go-recorded scenarios](../../../../conformance/connectors/google/drive/scenarios.json). The Dex integration tests use local provider stand-ins; live Google OAuth, domain delegation, Drive API, and `changes.list` polling behavior remain unverified.
