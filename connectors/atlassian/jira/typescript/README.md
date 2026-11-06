# Jira Cloud TypeScript connector

`@superdurable/connector-atlassian-jira` provides Dex Query and Mutation operations for Jira Cloud. Its manifest-generated Step factories cover `searchIssues`, `getIssue`, `createIssue`, `transitionIssue`, and `addComment`; no Trigger is declared. The package version is `0.1.0`, independent of the Go connector version.

Set up the Atlassian OAuth 2.0 application and Dex Web connection using the [connector guide](../README.md#atlassian-setup). The generated OAuth driver refreshes rotating tokens, and the project connection uses generated private and shared credential decoders. Credentials stay in project storage and outside Flow input and persisted output.

The [runnable issue read example](examples/get-issue/README.md) loads the static `jira-triage` connection, registers a real Worker, and reads one issue through `newGetIssueStep`. `createIssue` and `addComment` use Dex single-dispatch checkpoints and read-back before resolving an ambiguous write. Use the operation-specific Step factories and wire only the happy branch unless the application has a concrete business reaction to another outcome.

Run `npm run check` for TypeScript, unit, and Go-recorded conformance replay. Start a local `dexcli dev` stack and run `npm run test:integration` with `DEX_FLOW_SERVICE_ADDRESS` set to its gRPC address. The integration test uses a provider stand-in; live Atlassian OAuth and API behavior have not been verified.
