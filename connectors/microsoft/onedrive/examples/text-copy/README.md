# OneDrive and SharePoint text-copy example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every OneDrive and SharePoint operation:

1. `RecordTextCopyRequest` validates and persists the source, copy folder, and
   copy names;
2. `FindSourceFile` calls `onedrive.NewSearchFilesStep` with the exact source
   name in the saved source folder, which Graph reads by path;
3. `SelectSourceFile` records the found file; `ReportSourceNotFound` completes
   as `sourceNotFound` when the folder has no item with the name;
4. `EnsureCopyFolder` calls `onedrive.NewCreateFolderStep` in the saved
   destination folder, reusing a folder that already has the name, and
   `RecordCopyFolder` records it;
5. `ReadSourceText` calls `onedrive.NewReadFileTextStep`;
6. `PrepareTextCopy` records the text size and chooses `fail`, or `replace`
   when `shouldReplaceExisting` is true;
7. `UploadTextCopy` calls `onedrive.NewUploadFileStep` into the copy folder and
   keeps its Result in an Attribute; `ReportCopyAlreadyExists` completes as
   `copyAlreadyExists` when a different item already has the copy's name;
8. `ReadBackTextCopy` calls `onedrive.NewGetFileStep` on the copy;
9. `CompleteTextCopy` completes as `copied` with the read-back metadata.

Only the happy-path branches, `searchFiles`'s `notFound`, and `uploadFile`'s
`alreadyExists` are wired. Unsupported, oversized, rejected, invalid, and
defect outcomes fail the Flow. A source name that names a folder also fails it.

## Configure

Follow the [connector setup](../../README.md#authorization), then configure the
`microsoft-onedrive / onedrive-files` connection in Dex Web **Connections**.
Microsoft OAuth needs Dex CLI 1.4.1 or later. The example exposes two sets of
`sitePicker`, `drivePicker`, and `folderPicker` units; save each set in that
order:

- **Source** on `FindSourceFile`: the folder that directly contains the source
  file. Blank site and drive mean your own OneDrive; a blank folder means the
  drive root.
- **Destination** on `EnsureCopyFolder`: the folder in which the copy folder is
  ensured, with the same blank meanings.

App-only connections must save a drive ID in both sets. The Worker loads the
picks once at startup, so restart it after saving them.

## Run

Generate strict FDG 2.0 from `connectors/microsoft/onedrive` with the dexcli
release pinned in the repository's `.dex-compat-version` file:

```bash
mkdir -p build
dexcli visualize ./examples/text-copy/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/onedrive-text-copy
```

Run `dexcli dev` with that build directory, then run the Worker with the
connection path shown by Dex Web:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/text-copy
```

The default Worker address is `127.0.0.1:8823`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `OneDriveTextCopy` with a unique Flow ID:

```json
{
  "sourceName": "Ops Policy.txt",
  "copyFolderName": "Dex copies",
  "copyName": "Ops Policy (copy).txt"
}
```

The Flow output reports the source, the copy folder (`isCopyFolderExisting` is
true when it already existed), the text size, and the copy.
`isCopyExistingIdentical` is true when a file with exactly this text was
already at the copy's path, such as after a repeated dispatch.

```bash
GOWORK=off go test -race ./examples/text-copy/...
GOWORK=off go test -tags=integration ./examples/text-copy/... -count=1 -v
```

The integration tests run the Flow on a real Dex Server against the stateful
fake Graph in `internal/graphfake`, including a same-named decoy outside the
source folder, a hand-edited copy that blocks a `fail` upload, and a replace.
They hold the first upload or folder-creation response for nine seconds, so
Dex dispatches the Step twice, and prove one file or folder results.
