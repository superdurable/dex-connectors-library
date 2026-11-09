# Microsoft To Do for Dex Python

This package implements the `listTaskLists` and `listTasks` Queries from the [Microsoft To Do connector](../README.md). It uses delegated Microsoft Graph `Tasks.Read` authorization through the project connection and follows bounded collection pagination. Import `Client`, the typed inputs, and the generated Step factories from `dex_connectors.microsoft_todo`.

Run `uv run --project connectors/microsoft/todo/python pytest connectors/microsoft/todo/python/tests` from the repository root. Live Microsoft authorization is not included in the local tests.
