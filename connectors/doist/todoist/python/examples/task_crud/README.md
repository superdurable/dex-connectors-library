# Todoist Python task CRUD example

The `TodoistPythonTaskCRUD` Flow creates a disposable task, reads it, replaces
its title, lists its project to confirm it appears, then deletes it. Use a
Todoist account where creating and deleting a task is acceptable. The static
Dex connection name is `todoist`; enter a personal API token in Dex Web
Connections using the connector's authorization guide.

With `dexcli dev` running and `DEX_FLOW_SERVICE_ADDRESS` set to its gRPC address,
run from the repository root:

```sh
PYTHONPATH=connectors/doist/todoist/python uv run --project connectors/doist/todoist/python python -m examples.task_crud.worker
PYTHONPATH=connectors/doist/todoist/python uv run --project connectors/doist/todoist/python python -m examples.task_crud.start \
  todoist-python-example-1 connectors/doist/todoist/python/examples/task_crud/sample-input.json
```

Set `PYTHONPATH=connectors/doist/todoist/python` if running from the repository
root without the package's test environment. The example's integration test
uses a fake provider through the real Dex Worker and Client; it requires a
running `dexcli dev` instance. It does not authorize a real Todoist account.
