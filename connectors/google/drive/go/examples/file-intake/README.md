# Google Drive file-intake example

This example starts one Flow for every Drive file created in a configured
folder, using the durable `fileCreated` Trigger runner:

1. `drive.NewProjectFileTriggerRunner` polls the connection's Drive change log
   and records each matching `fileCreated` event in the binding's Trigger
   inbox before the shared cursor commits past it;
2. `sdkgo.NewDexFlowTriggerTarget` starts the `GoogleDriveFileIntake` Flow
   with the Flow ID `google-drive-file-intake-google-drive-files-<fileId>`, so
   a redelivered event starts no second Flow;
3. `RecordNewFile` stores the reported metadata;
4. `ReadNewFile` calls `drive.NewGetFileStep` for the file's current metadata;
5. `CompleteFileIntake` completes with the reported and current metadata.

Only the `found` branch of `ReadNewFile` is wired. A file deleted before the
Flow reads it, a rejected read, or an invalid response fails the Flow.

Files that existed when the binding first started never start a Flow. Edits,
trash, moves into the folder, and files in subfolders do not start one either.

## Configure

Follow the [Google Drive Connector setup](../../../README.md), then configure the
`google-drive / google-drive-files` connection in Dex Web **Connections**. The
`google-drive-file-intake-new-file` binding exposes two units:

- **Watched folder** (`folderPicker`): the folder whose direct children start
  the Flow. Blank starts a Flow for every new file the connection can see.
- **File type** (`mimeTypePicker`): the exact MIME type that starts the Flow,
  such as PDF. Blank matches every type, including folders.

The connection settings `pollInterval` and `pollPageSize` tune how often and
how much the Trigger reads. The runner loads the binding once at startup, so
restart the application after saving it; a binding that was never saved is a
startup error.

## Run

Generate strict FDG 2.0 from `connectors/google/drive` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/file-intake/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/google-drive-file-intake
```

Run `dexcli dev` with that build directory, then run the Worker and Trigger
runner. It reads the `DEX_PROJECT_*` project configuration environment
documented in
[`sdkgo/projectconfig`](../../../../../../sdkgo/projectconfig/README.md#application-loading),
or the Dex Web `connections.json` named by `DEX_CONNECTOR_CONFIG_FILE`:

```bash
go run ./examples/file-intake
```

Override `DEX_FLOW_SERVICE_ADDRESS` or `DEX_BLOB_CACHE_DIR` when needed. Under
`dexcli dev` with `connections.json`, the poll cursor is kept in process
memory, so a restart reseeds and files created while the runner was stopped
do not start a Flow; hosted project storage keeps the cursor.

## Test

```bash
go test ./examples/file-intake/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 go test -tags=integration ./examples/file-intake/... -count=1 -v
```

The integration test loads a Dex Web connections file with the binding
filtered to PDFs in one folder, runs the runner as `main.go` does against an
in-process Drive change log and a real Dex server, and checks that only the
matching new files start a Flow, that a file created while the runner is
stopped starts its Flow after the restart, and that each event is delivered
once.
