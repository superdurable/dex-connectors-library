# Dedao Get Note task status

`GetNoteTaskStatusFlow` calls `getNoteTaskStatus` through the static `dedao` Dex Web connection, persists the bounded task status Attribute, and completes with it. The optional `providerRejected`, `invalidResponse`, and `defect` branches are unwired, so selecting one fails the Flow. Temporary HTTP and transport errors retry under the Dex Step policy. A returned `failed` task status is a successful Query of a failed asynchronous task.

Create a connection named `dedao` in Dex Web. At [Get Note OpenAPI](https://www.biji.com/openapi), create an application and copy its secret `gk_live_` API Key and paired secret `cli_` Client ID to `apiKey` and `clientID`. Leave `timeout` (Go duration) and `maxResponseBytes` (bytes) blank for their displayed defaults. There are no operation configuration fields. Supply `data.tasks[0].task_id` from an earlier asynchronous note-save response as `taskID`; the provider recommends 10–30 second polling intervals.

From `connectors/dedao/python`, run `uv sync --locked`, start `dexcli dev`, and run `uv run python -m examples.get_note_task_status.worker` with the Dex project connection environment. Generate strict FDG 2.0 with:

```sh
uv run dexcli visualize examples/get_note_task_status/flow.py --language python --schema-version 2.0 --lint app --json --out /tmp/dex-dedao-python-task-status-fdg --open=false --python .venv/bin/python
```

With `DEX_FLOW_SERVICE_ADDRESS` pointing at a running Dex development server, `uv run pytest examples/get_note_task_status/integration_test.py` starts the Flow through a real Worker and Client, retries a synthetic provider 503, and checks the persisted status. The test uses no real Get Note credentials. Live provider authorization and task state remain unverified.
