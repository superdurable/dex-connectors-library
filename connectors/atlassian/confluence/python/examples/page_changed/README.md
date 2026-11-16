# Confluence page-changed Flow for Python

This example starts one `ConfluencePageChangePython` Flow for every page
created or updated in the picked space, through the `pageChanged` Trigger, at
parity with the [Go example](../../../go/examples/page-changed/README.md):

1. `RecordPageChange` validates the delivered change and records it.
2. `ReadChangedPage` calls `new_get_page_step` with the page ID and reads the
   page back as Markdown, at most 2,000 characters, so the record holds its
   current title and body.
3. `CompletePageChange` records the page read back and completes the Flow.

The Flow ID is `confluence-page-change-<Trigger event ID>`, and the event ID is
stable per page version, so a redelivered change finds its existing Flow. Only
the happy-path `found` branch is wired; a missing, rejected, or invalid read
fails the Flow. The first start records recently changed pages as a baseline
and starts no Flow for them.

## Configure

Follow the [Confluence connector setup](../../../README.md#atlassian-setup),
then configure the `confluence-page-watcher` connection in Dex Web
**Connections**. **Poll interval** is the delay between polls, at least 10
seconds; each poll normally makes one search request, which spends the OAuth
app's Confluence rate-limit quota. The `space-page-changes` binding of
`pageChanged` saves the watched space's ID, key, and name; leave it unsaved to
watch every space the account can view. Dex CLI v1.47.3 does not read the
Python binding's space picker, so Dex Web shows the binding without it. The
Worker reads saved values at startup, so restart it after saving; a changed
space records a new baseline.

## Run

From `connectors/atlassian/confluence/python`, generate strict FDG 2.0 and
start `dexcli dev` with it:

```sh
uv sync --locked
dexcli visualize ./examples/page_changed/flow.py --python .venv/bin/python \
  --schema-version 2.0 --json --out ./build/page-changed
dexcli dev -flow-rendering-dir ./build
```

Run the Worker and the Trigger with the connections file Dex Web wrote:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.page_changed.main
```

[`trigger.py`](trigger.py) wires the binding to the Flow:

```python
target = new_dex_flow_trigger_target(
    dex_client,
    flow,
    accepts_event=accepts_every_page_change,
    resolve_flow_id=resolve_flow_id,
    map_to_flow_input=map_to_flow_input,
    logger=logger,
)
return await new_project_page_trigger_runner(
    project,
    CONNECTION_NAME,
    [ProjectPageChangedTriggerRoute(PAGE_CHANGED_TRIGGER_BINDING, target)],
    logger=logger,
)
```

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the poll cursor commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline and changes made while it
was stopped are not delivered. Set `LOG_LEVEL` (`DEBUG`, `INFO`, `WARNING`) for
the `dex_connectors.sdk.trigger` records.

## Test

`tests/test_page_changed_example.py` covers the Flow's mapping, validation,
binding declaration, and the project runner's saved and unsaved bindings. With
`dexcli dev` running, the integration tests run a real Dex Worker and Client
against a local Confluence stand-in: the baseline starts no Flow, other spaces
are ignored, each page version starts one Flow across a Trigger restart whose
first search answers 429, and a changed space records a new baseline:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/page_changed/integration_test.py
```

Live Confluence search has not been verified.
