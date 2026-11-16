# Jira issue-changed Flow for Python

This example starts one `JiraIssueChangePython` Flow for every issue created or
updated in the picked project, through the `issueChanged` Trigger, at parity
with the [Go example](../../../go/examples/issue-changed/README.md):

1. `RecordIssueChange` validates the delivered change and records it.
2. `ReadChangedIssue` calls `new_get_issue_step` with the issue's stable ID, so
   the record holds the issue's current state.
3. `CompleteIssueChange` records the issue read back and completes the Flow.

The Flow ID is `jira-issue-change-<Trigger event ID>`, and the event ID is
stable per issue version, so a redelivered change finds its existing Flow. Only
the happy-path `found` branch is wired; a missing, rejected, or invalid read
fails the Flow. The first start records the project's existing issues as a
baseline and starts no Flow for them.

## Configure

Follow the [Jira Connector setup](../../../README.md), then configure the
`jira-issue-watcher` connection in Dex Web **Connections**. **Poll interval**
is the delay between polls, at least 5 seconds; each poll normally makes one
search request, which spends the app's hourly Jira rate-limit points. The
`project-issue-changes` binding of `issueChanged` saves the watched project's
ID, key, and name; leave it unsaved to watch every project the account can
browse. Dex CLI v1.47.3 does not read the Python binding's project picker, so
Dex Web shows the binding without it. The Worker reads saved values at startup,
so restart it after saving; a changed project records a new baseline.

## Run

From `connectors/atlassian/jira/python`, generate strict FDG 2.0 and start
`dexcli dev` with it:

```sh
uv sync --locked
uv run dexcli visualize ./examples/issue_changed/flow.py \
  --schema-version 2.0 --json --out ./build/issue-changed
dexcli dev -flow-rendering-dir ./build/issue-changed
```

Run the Worker and the Trigger with the connections file Dex Web wrote:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.issue_changed.main
```

[`trigger.py`](trigger.py) wires the binding to the Flow:

```python
target = new_dex_flow_trigger_target(
    dex_client,
    flow,
    accepts_event=accepts_every_issue_change,
    resolve_flow_id=resolve_flow_id,
    map_to_flow_input=map_to_flow_input,
    logger=logger,
)
return await new_project_issue_trigger_runner(
    project,
    CONNECTION_NAME,
    [ProjectIssueChangedTriggerRoute(ISSUE_CHANGED_TRIGGER_BINDING, target)],
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

`tests/test_issue_changed_example.py` covers the Flow's mapping, validation,
and binding declaration. With `dexcli dev` running, the integration tests run a
real Dex Worker and Client against a local stand-in for Jira's enhanced search:
the baseline starts no Flow, each issue version starts one Flow across a
Trigger restart, a 429 with `Retry-After` pauses polling without losing a
change, and a changed project records a new baseline:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/issue_changed/integration_test.py
```

Live Jira search has not been verified.
