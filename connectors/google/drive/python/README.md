# Google Drive Python connector

This package implements `searchFiles`, `getFile`, `readFileText`, and
`uploadFile`, and the `fileCreated`, `fileUpdated`, and `fileTrashed`
Triggers, for Dex Python applications. Both Google OAuth and Workspace
domain delegation use manifest-generated credential refresh and Dex project
storage. Configure `google / google-drive-files` in Dex Web **Connections**,
following the [connector authorization guide](../README.md). Do not put
access or refresh tokens in Flow input.
File timestamps follow Go JSON timestamp validation, including rejection of
hour 24 before a file Result is stored.

The [text copy example](examples/text_copy) searches by exact source name,
reads bounded UTF-8 text, uploads one copy behind a durable dispatch fence,
and reads its metadata back. It wires only the successful branch of each
operation. Save `connections.json` from Dex Web and use its path below.

From `connectors/google/drive/python`, generate the strict FDG 2.0 graph:

```sh
uv sync --locked
dexcli visualize examples/text_copy/flow.py --language python \
  --schema-version 2.0 --json --out build/google-drive-python-text-copy \
  --python .venv/bin/python
```

Start Dex with the generated graph, then run the Worker and Client entrypoints
from separate terminals:

```sh
dexcli dev -flow-rendering-dir build
```

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.text_copy.main
```

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.text_copy.start \
  google-drive-text-copy-001 examples/text_copy/sample-input.json
```

Use a new Flow ID for each start. The source must be a text file or a Google
Workspace document with a text export. The copy is written to My Drive root.
The example's `CONNECTION_NAME` remains `google-drive-files`.

## File Triggers

The three Triggers share one durable `changes.list` poller per connection, at
behavior parity with the Go connector's
[File Triggers](../README.md#file-triggers): the same classification, event
IDs, folder and MIME type filters, rate-limit pauses, and page-token reseeds.
`new_project_file_trigger_runner` opens a project connection, decodes each
selected binding's saved `FileTriggerConfiguration` with Go's strict JSON
rules, and records every event in the binding's project Trigger inbox before
the shared cursor commits. The cursor document and the inbox payloads are the
Go connector's, so Go and Python replicas can share them. The connection
settings `pollInterval` and `pollPageSize` (1 to 500) tune the poll.

- `new_file_trigger_runner(FileTriggerRunnerConfig(...))` takes an explicit
  `CursorStore`, such as `new_memory_cursor_store()`, for tests and
  applications without project storage.
- `new_file_created_trigger`, `new_file_updated_trigger`, and
  `new_file_trashed_trigger` poll one binding with an in-memory cursor, as Go's
  generated per-Trigger factories do; a restart reseeds.
- `define_file_created_trigger_binding` and its siblings declare a Flow's
  static bindings in `get_connector_trigger_bindings`.
- The runner raises `TriggerDeliveryAbortedError` after cancellation. Close a
  project runner with `aclose` after `run` returns.

The [file intake example](examples/file_intake) starts one Flow per created
file. [`intake.py`](examples/file_intake/intake.py) wires the binding:

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

The Triggers require `dex-connectors-sdk` 0.12.0 or later, which provides the
poll Trigger kit.

The Go-recorded provider scenarios and generated refresh agreement run with:

```sh
uv run pytest
```

With a real `dexcli dev` service at `DEX_FLOW_SERVICE_ADDRESS`, run the
checked-in Worker and Client integration test:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  uv run pytest examples/text_copy/integration_test.py examples/file_intake/integration_test.py
```

That test binds its fake provider to an OS-assigned free port. The OAuth consent
and Workspace delegated authorization flows against real Google accounts have
not been verified for this Python port.
