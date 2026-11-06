# Google Drive Python connector

This package implements `searchFiles`, `getFile`, `readFileText`, and
`uploadFile` for Dex Python applications. Both Google OAuth and Workspace
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

The Go-recorded provider scenarios and generated refresh agreement run with:

```sh
uv run pytest
```

With a real `dexcli dev` service at `DEX_FLOW_SERVICE_ADDRESS`, run the
checked-in Worker and Client integration test:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  uv run pytest examples/text_copy/integration_test.py
```

That test binds its fake provider to an OS-assigned free port. The OAuth consent
and Workspace delegated authorization flows against real Google accounts have
not been verified for this Python port.
