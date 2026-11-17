# Atlassian Confluence TypeScript connector

`@superdurable/connector-atlassian-confluence` provides Dex Query and Mutation
operations for Confluence Cloud pages and footer comments. Its five operations
are `searchPages`, `getPage`, `createPage`, `updatePage`, and `addComment`.
The public models, branch definitions, Step factories, connection helpers, and
mock cases are generated from `../connector.yaml`; OAuth credential refresh
uses the generated manifest declaration. The provider client and operation
behavior follow the Go connector, including page title and version read-back
for writes and a send-counting checkpoint that reads footer comments back before a resend within the send cap. From 0.2.0 the
package also implements the `pageChanged` poll Trigger, which starts a Flow
when a page is created or updated, on `@superdurable/connectors-sdk` 0.12.0's
poll Trigger kit.

A consuming application is checked in at
[examples/read-page](examples/read-page/README.md). It loads the private Dex
project connection `confluence-policy`, reads one page inside a Dex Step, and
returns only its ID, title, and version number. Run its real Dex integration
test with `DEX_FLOW_SERVICE_ADDRESS` set to the local `dexcli dev` gRPC address.

## pageChanged Trigger

`pageChanged` polls Confluence CQL content search, newest modification first,
at parity with the Go connector, on `@superdurable/connectors-sdk/pollingtrigger`;
the [connector README](../README.md#pagechanged-trigger) describes the query,
the ten-minute lookback, the keyed snapshot of seen page versions, paging,
change kinds, resets, and rate limits. The cursor token and snapshot entries
match the Go connector's byte for byte, including nanosecond times, which the
Go-generated fixtures in `test/fixtures/go-page-change-cases.json` check, so a
Go and a TypeScript replica can share one binding's cursor and inbox.

- `newProjectPageTriggerRunner(project, connectionName, routes, options)` opens
  the connection and each route's saved binding and returns a
  `PageTriggerRunner`. Every change is recorded in the binding's project
  Trigger inbox before its project poll cursor commits, so a restarted Worker
  neither misses nor repeats a change. An unsaved binding watches every space
  the connection can view. Await `run(context)` for the life of the Worker;
  `wakePoller(bindingName)` polls now.
- `newPageTriggerRunner({ connection, pageChangedRoutes })` takes targets and
  cursor stores directly and returns the kit's `Runner`; a route without a
  cursor store keeps it in memory and records a new baseline on every start,
  as `newPageChangedTrigger` always does.
- Each `PageChangedEvent` carries the site, the derived `changeKind`
  (`PageChangeCreated` for version 1, `PageChangeUpdated` otherwise), and the
  page summary, whose `lastModifiedAt` keeps Confluence's time exactly as Go
  writes it. The event ID hashes the site, page ID, and version, so
  `newDexFlowTriggerTarget` starts one Flow per page version. The event's
  `occurredAt` is a `Date`, at millisecond precision.
- The connection's `pollInterval` (at least 10 seconds; zero keeps the
  one-minute default) sets the poll delay, and `ConfluenceClientOptions`
  `triggerLogger` receives the runner's records.
- `definePageChangedTriggerBinding` declares a static binding with the
  `spacePicker` unit from `getConnectorTriggerBindings`.

The runnable [page-changed Flow](examples/page-changed/README.md) starts one
Flow per page version and reads the page back as Markdown. Its integration
tests run a real Dex Worker and Client against a local Confluence stand-in:
the baseline starts no Flow, other spaces are ignored, each page version
starts one Flow across a Trigger restart whose first search answers 429, and a
changed space records a new baseline. Live Confluence search has not been
verified.

Run `npm ci && npm run check` to build and test the package. The conformance
suite replays the recorded Go provider scenarios under
`../../../../conformance/connectors/atlassian/confluence/scenarios.json`.
Live Atlassian authorization, site selection, writes, and comment delivery
remain unverified.
