# Microsoft To Do for Dex TypeScript

This package implements the `listTaskLists` and `listTasks` Queries from the [Microsoft To Do connector](../README.md). Import `MicrosoftTodoClient`, typed inputs, and generated Step factories from `@superdurable/connector-microsoft-todo`. The project connection uses delegated Graph `Tasks.Read` authorization.

`npm run check --prefix connectors/microsoft/todo/typescript` verifies types, lint, tests, and build. Live Microsoft authorization is not included in local tests.
