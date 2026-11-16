# Google Drive file intake

The [Flow](flow/workflow.ts) starts once for each file created in the folder
that the `google-drive-file-intake-new-file` binding of the static
`google-drive-files` connection selects. It records the reported metadata,
reads the file's current metadata with `getFile`, and completes with both. It
never reads file content, and only the happy `found` branch is wired.

Configure `google-drive / google-drive-files` in Dex Web **Connections** using
the [connector authorization guide](../../../README.md), then configure the
binding's two units. **Watched folder** lists the authorized account's folders
and saves the folder ID and name; only files directly inside it count, and
blank watches every folder. **File type** saves one exact lowercase MIME type,
such as `application/pdf`; blank matches every type, including folders.

Generate the strict FDG 2.0 reference, which carries the static binding and
its units, from `connectors/google/drive/typescript`:

```sh
npm run build
npm install --prefix examples/file-intake --no-save --no-package-lock --ignore-scripts .
cd examples/file-intake
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Start `dexcli dev` with unused gRPC and Web ports, set
`DEX_FLOW_SERVICE_ADDRESS` to its gRPC address and `DEX_CONNECTOR_CONFIG_FILE`
to the `connections.json` that Dex Web wrote, and run the Worker and the
durable Trigger runner:

```bash
npm run example:intake
```

[`app.ts`](app.ts) wires the binding:

```ts
return newProjectFileTriggerRunner(project, ConnectionName, { fileCreatedRoutes: [{ bindingName: NewFileTriggerBinding, target }] }, options);
```

Each event is recorded in the binding's durable inbox before the shared cursor
commits, and each file starts the Flow
`google-drive-typescript-file-intake-<connection>-<file ID>` with the event ID
as its request ID, so a redelivered event starts no second Flow. Files that
existed when the runner first started never start a Flow. Under `dexcli dev`
the poll cursor lives in process memory, so a process restart reseeds; hosted
project storage keeps it.

The [integration test](integration.test.ts) runs the example against a real
Dex Worker and Client and the in-process [Drive change log](../../test/drive_change_log.ts):
an existing file, a Google Doc, a file in another folder, and an edit start
nothing; two created PDFs start one Flow each, one of them created while the
runner was stopped; and the restart resumes the stored cursor. Run it with
`npm run test:integration`. Polling a live Google Drive account is unverified.
