# Jira Cloud TypeScript connector

`@superdurable/connector-atlassian-jira` provides Dex Query and Mutation operations for Jira Cloud. Its manifest-generated Step factories cover `searchIssues`, `getIssue`, `createIssue`, `transitionIssue`, and `addComment`, and from 0.2.0 it implements the `issueChanged` poll Trigger. The package version is `0.2.1`, independent of the Go connector version. Install it with `@superdurable/connectors-sdk@0.12.0`, whose poll Trigger kit the Trigger is built on, and `@superdurable/dex` 1.45.4 or newer.

Set up the Atlassian OAuth 2.0 application and Dex Web connection using the [connector guide](../README.md#atlassian-setup). The generated OAuth driver refreshes rotating tokens, and the project connection uses generated private and shared credential decoders. Credentials stay in project storage and outside Flow input and persisted output.

The [runnable issue read example](examples/get-issue/README.md) loads the static `jira-triage` connection, registers a real Worker, and reads one issue through `newGetIssueStep`. `createIssue` and `addComment` use Dex single-dispatch checkpoints and read-back before resolving an ambiguous write. Use the operation-specific Step factories and wire only the happy branch unless the application has a concrete business reaction to another outcome.

## issueChanged Trigger

`issueChanged` polls Jira's enhanced search on the `updated` time at parity with the Go connector, on `@superdurable/connectors-sdk/pollingtrigger`; the [connector README](../README.md#issuechanged-trigger) describes the query, the two-minute lookback, paging, change kinds, resets, and rate limits. The cursor tokens match the Go connector's byte for byte, which the Go-generated fixtures in `test/fixtures/go-issue-change-cases.json` check, so a Go and a TypeScript replica can share one binding's cursor and inbox. One deviation: a stored integer beyond 2^53, which Go's int64 holds, makes the cursor invalid and records a new baseline, as the SDK's poll cursor documents do.

- `newProjectIssueTriggerRunner(project, connectionName, routes, options)` opens the connection and each route's saved binding. Every change is recorded in the binding's project Trigger inbox before its project poll cursor commits, so a restarted Worker neither misses nor repeats a change. An unsaved binding watches every project the account can browse. Await `run(context)` for the life of the Worker; `wakePoller(bindingName)` polls now. The connection's `pollInterval` (at least 5 seconds) sets the schedule, and the client options' `triggerLogger` receives the poll and inbox records.
- `newIssueTriggerRunner({ connection, issueChangedRoutes })` takes targets and cursor stores directly and returns the kit's `Runner`; a route without a cursor store keeps it in memory and records a new baseline on every start, as `newIssueChangedTrigger` always does.
- Each `IssueChangedEvent` carries the site, the derived `changeKind` (`created` or `updated`), and the issue's standard fields. The event ID hashes the site, issue ID, and `updated` instant (`issueChangeEventId`), so `newDexFlowTriggerTarget` starts one Flow per issue version.
- `defineIssueChangedTriggerBinding` declares a static binding with its `projectPicker` unit for Dex Web.

The runnable [issue-changed example](examples/issue-changed/README.md) starts one Flow per change and reads the issue back.

Run `npm run check` for TypeScript, unit, and Go-recorded conformance replay. Start a local `dexcli dev` stack and run `npm run test:integration` with `DEX_FLOW_SERVICE_ADDRESS` set to its gRPC address. The integration tests use a provider stand-in: the issue-changed tests run the example against a real Dex Worker and Client, covering the baseline, one Flow per issue version across a restart, a 429 with `Retry-After`, and a changed project that records a new baseline. Live Atlassian OAuth, API behavior, and live Jira search have not been verified.
