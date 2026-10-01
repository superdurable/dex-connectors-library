# Google Drive text-copy example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Google Drive operation:

1. `RecordTextCopyRequest` validates and persists the source and copy names;
2. `FindSourceFile` calls `drive.NewSearchFilesStep` with the exact source name
   in the saved source folder, excluding trashed files;
3. `SelectSourceFile` continues only when exactly one file matches and Drive
   searched completely; otherwise the Flow completes as `sourceAmbiguous` with
   the candidates;
4. `ReportSourceNotFound` completes as `sourceNotFound` when nothing matches;
5. `ReadSourceText` calls `drive.NewReadFileTextStep`;
6. `PrepareTextCopy` records the text size and builds the upload;
7. `UploadTextCopy` calls `drive.NewUploadFileStep` into the saved destination
   folder and keeps its Result in an Attribute;
8. `ReadBackTextCopy` calls `drive.NewGetFileStep` on the new file;
9. `CompleteTextCopy` completes as `copied` with the read-back metadata.

Only the happy-path Google Drive branches and `searchFiles`'s `notFound` are
wired. Unsupported, oversized, rejected, invalid, uncertain, and defect
outcomes fail the Flow, and an uncertain upload is never sent again.

## Configure

Follow the [Google Drive Connector setup](../../README.md), then configure the
`google-drive / google-drive-files` connection in Dex Web **Connections**. The
example exposes two `folderPicker` units:

- **Source folder** on `FindSourceFile`: the folder whose direct children are
  searched. Blank searches every folder the connection can see.
- **Destination folder** on `UploadTextCopy`: the folder that receives the
  copy. Blank creates the copy in the My Drive root.

The Worker loads both picks once at startup, so restart it after saving them.

## Run

Generate strict FDG 2.0 from `connectors/google/drive` with the dexcli release
pinned in the repository's `.dex-compat-version` file:

```bash
mkdir -p build
dexcli visualize ./examples/text-copy/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/google-drive-text-copy
```

Run `dexcli dev` with that build directory, then run the Worker with the
connection path shown by Dex Web:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/text-copy
```

The default Worker address is `127.0.0.1:8822`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `GoogleDriveTextCopy` with a unique Flow ID:

```json
{
  "sourceName": "Ops Policy",
  "copyName": "Ops Policy (text copy).txt"
}
```

The source must be a Google Doc, Sheet, or Slides file, or a text file within
the connection's `maxTextBytes`. The Flow output reports the resolved source,
the text MIME type and size, and the uploaded copy. `isCopyFromEarlierAttempt`
is true when a retried upload reused the file an earlier attempt created.

```bash
GOWORK=off go test -race ./examples/text-copy/...
GOWORK=off go test -tags=integration ./examples/text-copy/... -count=1 -v
```

The integration tests run the Flow on a real Dex Server against a stateful
fake Drive provider with the research fixture's decoys: a trashed draft and an
archived twin with the same name, and two current files that make a name
ambiguous. They also prove that a retried upload reuses the file an earlier
attempt created and that an uncertain upload fails without a second upload.
