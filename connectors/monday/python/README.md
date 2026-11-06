# monday.com Python connector

`dex-connector-monday` exposes typed `list_items`, `get_item`, `create_item`, `update_item_column_values`, and `add_update` operations through Dex Steps. Project connections accept a personal API token or monday.com OAuth 2.1; the generated refresh driver handles OAuth token renewal.

See the [work-request example](examples/work_request/README.md) for a runnable Flow and setup instructions. The example uses the static connection name `monday-work-requests` and leaves optional provider branches unwired, so they fail the Flow for review.
