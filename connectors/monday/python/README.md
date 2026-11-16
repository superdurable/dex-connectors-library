# monday.com Python connector

`dex-connector-monday` exposes typed `list_items`, `get_item`, `create_item`, `update_item_column_values`, and `add_update` operations through Dex Steps, and the `itemChanged` poll Trigger. Project connections accept a personal API token or monday.com OAuth 2.1; the generated refresh driver handles OAuth token renewal.

See the [work-request example](examples/work_request/README.md) for a runnable Flow and setup instructions. The example uses the static connection name `monday-work-requests` and leaves optional provider branches unwired, so they fail the Flow for review.

## itemChanged Trigger

`itemChanged` delivers each active item created or changed on one board, at behavior parity with the Go connector on the `dex-connectors-sdk` poll Trigger kit (`dex_connectors.sdk.pollingtrigger`). Each poll reads `items_page` ordered by `__last_updated__` descending, 100 items per page, then `next_items_page` until an item older than the cursor's last-updated time minus a five-minute lookback, at most 100 pages. The first poll records the newest last-updated time as a baseline and delivers nothing; an empty board records Go's zero time, so every later item is delivered.

- The cursor token `{"boardId":...,"watermark":...}`, the keyed snapshot of item content hashes inside the lookback window, and the Trigger event IDs match Go byte for byte, so Go and Python replicas share one binding's cursor and inbox. `tests/test_item_changed_go_parity.py` replays a recording of the Go fetcher.
- Changes go oldest first in batches of at most 100 items and about 4 MiB of item JSON; the rest of a burst comes from the same scan. `change_kind` is `created` for an item the snapshot has not seen whose `createdAt` is inside the window, else `updated`.
- An undecodable cursor, a cursor for another board, or more than 10,000 changes since the last poll records a new baseline and logs a WARN record; changes in that gap are not delivered.
- HTTP 429 and monday.com's rate-limit codes pause every binding of the connection for `Retry-After` or `retry_in_seconds`; `DAILY_LIMIT_EXCEEDED` pauses it until midnight UTC. Other failures back off per binding.
- The connection's `pollInterval` (`Client.poll_interval`, default five minutes) sets the delay between polls; `Client` refuses one under 30 seconds.

| API | Use |
| --- | --- |
| `new_project_item_trigger_runner(project, connection, [ProjectItemChangedTriggerRoute(...)])` | Durable runner: loads each binding's saved board, records changes in the binding's project Trigger inbox before its project poll cursor commits; a binding without a saved board raises `ValueError`. |
| `new_item_trigger_runner(connection, [ItemChangedTriggerRoute(...)])` | Runner over explicit board configurations, targets, and cursor stores. |
| `new_item_changed_trigger(...)` | One binding with an in-memory cursor, like Go's generated `NewItemChangedTrigger`; every start records a new baseline. |
| `define_item_changed_trigger_binding(...)` | Declares the binding in a Flow's `get_connector_trigger_bindings`. |

The [item-changed example](examples/item_changed/README.md) is a runnable Worker that starts one Flow per change and reads the item back. Live monday.com polling has not been verified.
