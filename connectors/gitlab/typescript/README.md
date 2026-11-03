# GitLab TypeScript connector

`@superdurable/connector-gitlab` supplies the `getProject`, `listIssues`, and `updateIssueLabels` operations for Dex Flows. The package is generated from `../connector.yaml`; `src/client.ts`, `src/operations.ts`, and `src/project.ts` own the provider adapter.

Use `newProjectConnection(project, ConnectionName)` to bind the `gitlab` connection declared in your Dex project. The runnable [get-project example](examples/get-project/README.md) shows the Worker and starter. It uses `DEX_FLOW_SERVICE_ADDRESS` when set. Configure `access_token` in Dex Web Connections; never place it in Flow input. The token needs `read_api` for reads and `api` for label updates.

`updateIssueLabels` replaces the complete label set. An empty, null, or omitted `labels` value sends an empty form field and clears all labels.

Run `npm ci && npm run check` for the package suite. For a real Dex integration test, start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address before `npm run test:integration`.
