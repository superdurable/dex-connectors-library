# Python LLM summarize-text example

This runnable Flow uses the static `llm-summarizer` connection. Its Worker
loads the user-controlled Dex project, and each generation resolves the key
from that connection. `SummarizeTextInput` has one nonsecret `text` field.
The generated text is persisted in the `llm-python-summary` Attribute and
returned as the Flow output.

Generate the strict FDG 2.0 definition from the checked-in Go example. From
`connectors/superdurable/llm`, run:

```sh
mkdir -p build
dexcli visualize ./go/examples/summarize-text/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/summarize-text
```

The generated JSON should contain `"valid": true`. The local module can also report
`connector_release_required` until its release metadata is available. Start
`dexcli dev -flow-rendering-dir ./build`, then create `llm-summarizer` in Dex
Web **Connections**. Follow the [provider authorization guide](../../../README.md#connection),
save the connection, and use the owner-only `connections.json` project file
that Dex Web produces. The Python package's connector descriptor is generated
from the same manifest.

From `connectors/superdurable/llm/python`, run the Worker:

```sh
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
uv run python -m examples.summarize_text.worker
```

Start a unique Flow ID with the checked-in Client entrypoint:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
uv run python -m examples.summarize_text.start \
  llm-summary-001 examples/summarize_text/sample-input.json
```

The start input never selects a model or controls billing. The Step uses the
model chosen on the connection. Only the normal `generated` branch is wired;
other definite outcomes fail the Flow with a safe reason.

With `dexcli dev` running, verify the same Python Flow against a real Dex
Worker and Client and a loopback provider stand-in bound to an OS-assigned
port:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
uv run pytest -q examples/summarize_text/integration_test.py
```
