# Notion TypeScript connector

`@superdurable/connector-notion@0.2.1` implements Notion's `search`, `queryDatabase`, and `getPage` Queries, `createPage` and `updatePageProperties` Mutations, and, from 0.2.0, the `databaseRowChanged` poll Trigger. It requires `@superdurable/connectors-sdk` 0.12.0 and `@superdurable/dex` 1.45.4 or newer. The manifest generates operation models, Step factories, connection bindings, mock cases, and `dex-connector.json`. Import scripted mocks from `@superdurable/connector-notion/mock`.

## Configure a connection

Create a Notion internal connection at [Notion's developer portal](https://app.notion.com/developers/connections) under **Build → Internal connections**. Enable **Read content**, **Update content**, and **Insert content**, then share each database or page with the connection. Save its API token in the secret `api_token` field in Dex Web **Connections**. Keep the token out of Flow inputs, Attributes, and logs. `endpoint`, `maxResponseBytes`, `pollInterval`, and `pollPageSize` may be left blank for the manifest defaults; `pollInterval` must be at least 5 seconds and `pollPageSize` 1 to 100.

A database contains one or more data sources. `queryDatabase` and `createPage` accept either a data source ID or a database ID; a database ID triggers one lookup and requires exactly one data source. A page ID accepts dashed or compact hex and a Notion page URL. Search is title-only and may lag recently created pages.

## Run the example

The [search-pages example](examples/search-pages/README.md) loads the static `notion` connection and runs the generated `newSearchStep` through a Dex Worker. It persists the request and result as Attributes. The optional `noMatch` and rejection branches remain unwired, so selecting one fails the Flow. The API token stays in the connection credential source.

## databaseRowChanged Trigger

`databaseRowChanged` delivers each row created or edited in one data source, at parity with the Go Trigger, on the TypeScript Connector SDK's durable poll Trigger kit (`@superdurable/connectors-sdk/pollingtrigger`). It polls `POST /v1/data_sources/{id}/query` with a `last_edited_time` `on_or_after` filter, sorted ascending, and needs only the **Read content** capability.

- **Baseline.** The first poll reads the newest `last_edited_time` with one descending one-row query, then remembers every row edited in the two minutes before it, so no existing row is delivered. An empty data source starts at the Unix epoch, not the host clock.
- **Cursor.** The cursor holds the newest `last_edited_time` read, 96-bit digests of the rows seen inside the two-minute lookback, and Notion's `start_cursor` while a query spans pages. Each poll queries from the high water minus the lookback, so shared, rounded, or late-indexed timestamps are delivered once. At most 2,000 seen rows and 48 KiB of them are kept; an evicted row can repeat with the same event ID, which a Dex Flow target does not start twice.
- **Events.** `DatabaseRowChangedEvent` carries the data source ID, the row as `Page`, and `changeKind` (`DatabaseRowChangeKind`): `created` when `created_time` equals `last_edited_time`, else `updated`. The event ID hashes the data source, row ID, and `last_edited_time`. Trashed and archived rows are not returned by the query, so they are not delivered.
- **Limits.** Each request reads `pollPageSize` rows and a poll follows at most ten pages; a sequence ends at Notion's 10,000-row query limit.
- **Failures.** 429 `rate_limited` and 529 `service_overload` pause every binding of the connection for `Retry-After`. A 400 `validation_error` for a stored `start_cursor` restarts the query without a gap; a cursor for another data source or an undecodable one records a new baseline. A 404, a revoked token, or `public_api_request_blocked` backs off per binding.

`newProjectDatabaseRowTriggerRunner(project, connectionName, config, options)` opens the project connection and records each change in the binding's project Trigger inbox before the cursor commits in its project poll cursor; it requires a saved data source. `newDatabaseRowTriggerRunner` takes targets and cursor stores directly, and `newDatabaseRowChangedTrigger` keeps the cursor in memory, so each start records a new baseline. `defineDatabaseRowChangedTriggerBinding` declares the binding, and `decodeDatabaseRowChangedEventPayload` decodes inbox payloads written by Go, Python, or TypeScript. `NotionClientOptions.logger` receives the poll records, never row contents or the API token.

The fetcher sends the same queries, commits the same cursor documents byte for byte, and derives the same Trigger event IDs as Go, so Go and TypeScript replicas can share one binding's cursor and inbox; `test/database_row_go_fixtures.test.ts` replays polls recorded from the Go fetcher. The [row-changed example](examples/row-changed/README.md) starts one `NotionDatabaseRowChangeTypeScript` Flow per row version and has real Dex integration tests.

## Failure behavior and verification

Reads retry transient transport, 408, 409, 429, 529, and server errors. `updatePageProperties` repeats absolute values safely and retries transient failures. `createPage` uses the Connector SDK single-dispatch fence with the Step's send cap: from 0.2.1 an unknown outcome, such as a timeout, 409, 529, or 5xx without a committed ID, is resent while possible sends stay below the cap and selects `uncertain` at the cap, an unreadable checkpoint returns Retry, and a known accepted ID is read back instead of posting again. Conclusive provider rejections select operation branches. Notion error message text is not exposed.

Run `npm ci && npm run check`. With a local `dexcli dev` service, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration`. The conformance suite replays Go-recorded provider requests and results. Live Notion authorization, response behavior, and data source query polling remain unverified.
