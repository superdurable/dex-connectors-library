# monday.com TypeScript connector

`@superdurable/connector-monday` provides generated Dex Step factories and
mock cases for `listItems`, `getItem`, `createItem`, `updateItemColumnValues`,
and `addUpdate`, and the `itemChanged` poll Trigger. The client resolves personal API tokens or host-refreshed OAuth
2.1 credentials at invocation time. OAuth token exchange and credential
serialization use the manifest-generated SDK declaration. No credential enters
Flow input, output, Attributes, or receipts.

The [list-items example](examples/list-items/README.md) is a runnable consuming
application with the static `monday-board` connection. Use its Worker and starter
to read a real board, or run `npm run integration:list` with `dexcli dev` for a
credential-free Worker and Client integration test. `npm run check` runs the
package suite, typecheck, and lint; the package suite replays Go-recorded
provider conformance scenarios.

Configure provider connection fields through Dex Web Connections; the
connector's root README and `connector.yaml` document the personal token and
OAuth setup paths.

## itemChanged Trigger

`itemChanged` delivers each active item created or changed on one board, at
behavior parity with the Go connector on the `@superdurable/connectors-sdk` poll
Trigger kit (`@superdurable/connectors-sdk/pollingtrigger`). Each poll reads
`items_page` ordered by `__last_updated__` descending, 100 items per page, then
`next_items_page` until an item older than the cursor's last-updated time minus
a five-minute lookback, at most 100 pages. The first poll records the newest
last-updated time as a baseline and delivers nothing; an empty board records
Go's zero time, so every later item is delivered.

- The cursor token `{"boardId":...,"watermark":...}`, the keyed snapshot of
  item content hashes inside the lookback window, and the Trigger event IDs
  match Go byte for byte, so Go and TypeScript replicas share one binding's
  cursor and inbox. `test/item_changed_go_parity.test.ts` replays a recording
  of the Go fetcher.
- Changes go oldest first in batches of at most 100 items and about 4 MiB of
  item JSON; the rest of a burst comes from the same scan. `changeKind` is
  `created` for an item the snapshot has not seen whose `createdAt` is inside
  the window, else `updated`.
- An undecodable cursor, a cursor for another board, or more than 10,000
  changes since the last poll records a new baseline and logs a WARN record;
  changes in that gap are not delivered.
- HTTP 429 and monday.com's rate-limit codes pause every binding of the
  connection for `Retry-After` or `retry_in_seconds`; `DAILY_LIMIT_EXCEEDED`
  pauses it until midnight UTC. Other failures back off per binding.
- The connection's `pollInterval` (`MondayClient.pollIntervalMs`, default five
  minutes) sets the delay between polls; `MondayClient` refuses one under 30
  seconds.

| API | Use |
| --- | --- |
| `newProjectItemTriggerRunner(project, connection, [{ bindingName, target }])` | Durable runner: loads each binding's saved board and records changes in the binding's project Trigger inbox before its project poll cursor commits; a binding without a saved board throws. |
| `newItemTriggerRunner(connection, [{ bindingName, configuration, target, cursor }])` | Runner over explicit board configurations, targets, and cursor stores. |
| `newItemChangedTrigger({ connection, bindingName, configuration, target })` | One binding with an in-memory cursor, like Go's generated `NewItemChangedTrigger`; every start records a new baseline. |
| `defineItemChangedTriggerBinding({ connectionName, bindingName, configurationUI })` | Declares the binding in a Flow's `getConnectorTriggerBindings`. |

The [item-changed example](examples/item-changed/README.md) is a runnable
Worker that starts one Flow per change and reads the item back. Live
monday.com polling has not been verified.
