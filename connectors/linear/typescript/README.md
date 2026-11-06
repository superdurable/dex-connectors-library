# Linear TypeScript connector

`@superdurable/connector-linear` provides the seven Linear GraphQL operations declared in `connector.yaml` for Dex TypeScript Flows: `searchIssues`, `getIssue`, `createIssue`, `updateIssue`, `addComment`, `findUserByEmail`, and `listWorkflowStates`. The signed `issueEventReceived` webhook Trigger remains Go only.

Install with `npm install @superdurable/connector-linear@0.1.0`. The package requires `@superdurable/connectors-sdk@0.5.0` and Node.js 22 or 24. Generated models, Step factories, mock outcomes, OAuth refresh declarations, and credential decoders are under `src/generated/`; edit the manifest or Go source and rerun `connectorctl generate` instead of editing those files.

## Connection and example

Create a Linear personal API key at <https://linear.app/settings/account/security> under **Personal API keys > New API key**, or configure a Linear OAuth application at <https://linear.app/settings/api/applications/new>. In Dex Web **Connections**, follow the manifest's authorization guide, select the static connection name `linear-issue-search`, and leave the optional configuration fields blank for their displayed defaults. OAuth refresh, rotating refresh token storage, and reauthorization use the generated manifest refresh declaration. The personal key is sent as a raw `Authorization` header; OAuth uses `Bearer`.

The runnable [issue search example](examples/issue-search/README.md) opens `newProjectConnection(project, ConnectionName)` in [main.ts](examples/issue-search/main.ts), binds `newSearchIssuesStep`, persists the page, and completes. Only its happy `searched` branch is wired. Other optional branches fail the Flow. The same package exports typed factories and `newConnection` for applications that supply a client directly.

## Provider and retry behavior

Queries use bounded named GraphQL documents and classify HTTP 429 or 5xx as Retry. Writes use the Dex Call ID to derive stable client UUIDs for creates and comments; updates set absolute values. Create and comment operations read back by those IDs after an ambiguous answer. A conclusive provider rejection chooses its optional branch. Credentials, provider response text, and OAuth refresh tokens never enter Flow state or receipts.

Run `npm ci && npm run check` for the TypeScript package, then `make test-connector-conformance CONNECTOR_DIRECTORY=connectors/linear` from the repository root for Go-recorded provider replay. For the real Dex integration test, start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` before `npm run test:integration`. The provider stand-in listens on an OS-assigned port. Live Linear workspace authorization, issue writes, token refresh, and webhook delivery have not been verified by this TypeScript package.
