# Google Drive file intake Flow for Python

This runnable example starts the `GoogleDriveFileIntakePython` Flow once for
each file created in the folder that the `google-drive-file-intake-new-file`
binding of the static `google-drive-files` connection selects. The Flow
records the reported metadata, reads the file's current metadata with
`getFile`, and completes with both. It never reads file content.

From `connectors/google/drive/python`, generate the Flow's strict FDG 2.0
reference, which carries the static binding, and start `dexcli dev` with it:

```sh
uv sync --locked
dexcli visualize ./examples/file_intake/flow.py --language python \
  --schema-version 2.0 --json --out ./build/file-intake --python .venv/bin/python
dexcli dev -flow-rendering-dir ./build
```

1. In Dex Web **Connections**, configure `google-drive / google-drive-files`
   with Google OAuth or Workspace domain-wide delegation, following the
   [connector authorization guide](../../../README.md).
2. Configure the `google-drive-file-intake-new-file` binding of
   `fileCreated`. `folderId` is the ID of the watched folder, the last path
   segment of its `https://drive.google.com/drive/folders/<ID>` link; only
   files directly inside it count. `mimeType` is one exact lowercase type such
   as `application/pdf`. Leave either blank to match every folder or type.
   Dex CLI v1.47.3 reads a Python binding's configuration UI only as a plain
   dict literal, which the typed `configuration_ui` argument does not accept,
   so this Python example declares no folder or file type picker units; the
   Go example declares both.
3. Set `DEX_CONNECTOR_CONFIG_FILE` to the owner-only `connections.json` that
   Dex Web wrote. The binding's durable inbox is kept beside it.
4. Run the Worker and the Trigger runner on one event loop:

   ```sh
   DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
     DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
     uv run python -m examples.file_intake.main
   ```

5. Upload a PDF into the watched folder. Files that existed when the runner
   first started never start a Flow.

[`intake.py`](intake.py) wires the Trigger to the Flow:

```python
return await new_project_file_trigger_runner(
    project,
    CONNECTION_NAME,
    ProjectFileTriggerRunnerConfig(
        file_created_routes=[ProjectFileTriggerRoute(NEW_FILE_TRIGGER_BINDING, target)]
    ),
    logger=logger,
)
```

Each file starts the Flow `google-drive-python-file-intake-<connection>-<file ID>`
with the event ID as the request ID, so a redelivered event is a duplicate
start, not a second run. The Trigger log records, which never carry file
names, page tokens, or credentials, go to the `dex_connectors.sdk.trigger`
logger, which `main.py` sets to INFO. Under `dexcli dev` the poll cursor lives
in process memory, so a process restart reseeds; hosted project storage keeps
it.

With `dexcli dev` running, the integration test runs the example against a
real Dex Worker and Client and an in-process Drive change log
([`drive_change_log.py`](drive_change_log.py)): an existing file, a Google
Doc, a file in another folder, and an edit start nothing; two created PDFs
start one Flow each, one of them created while the runner was stopped; and the
restart resumes the stored cursor:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  uv run pytest examples/file_intake/integration_test.py
```

Polling a live Google Drive account has not been verified.
