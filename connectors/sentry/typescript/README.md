# Sentry TypeScript connector

Install `@superdurable/connector-sentry` to use Sentry issue operations in Dex TypeScript Flows. The package implements `listIssues`, `getIssue`, and `updateIssueStatus`; the manifest's Go connector version remains independent of this package's `v0.1.0` release.

`SentryClient` accepts generated `Config` and a live `CredentialProvider<Credentials>`. It resolves the secret at invocation time and sends it only in the Authorization header. `newProjectConnection` reads a named connection and its `api_key` from the Dex project. `newListIssuesStep`, `newGetIssueStep`, and `newUpdateIssueStatusStep` are the generated operation factories. `./mock` exports scripted operations.

The runnable [issue-review example](examples/issue-review/README.md) lists one organization issue page through a Dex Worker. The issue search query is provider syntax; the example sends no credentials in Flow input. The status mutation uses an absolute PUT and a stable Call ID key. Local invalid input or unavailable credentials select `defect`; 408, 429, 5xx, and transport failures retry. Definite provider rejection, missing detail resources, and malformed successful responses select their manifest branches.

`npm run check` runs the package suite, typecheck, and lint. `npm run test:integration` requires a local `dexcli dev` address in `DEX_FLOW_SERVICE_ADDRESS` and exercises the example against a local HTTP fixture through a real Dex Worker. Go-recorded scenarios in `conformance/connectors/sentry/scenarios.json` are replayed by the TypeScript suite. Real Sentry authorization, API responses, and Dex Web Connections fields remain unverified.
