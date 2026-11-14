# RongCloud user online status Flow

This runnable Flow uses the static `rongcloud` connection to check one IM
user's online status. The `checked` branch stores only the `isOnline` result
in a Dex Attribute and completes with that boolean. Optional branches are
unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/rongcloud/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/user-online-status/flow/workflow.go --schema-version 2.0 --json --out ./build/user-online-status
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to
`dexcli dev --connector-release-override rongcloud=<artifact-directory>`.
In Dex Web **Connections**, create the `rongcloud` connection with the App Key
and App Secret from a domestic [RongCloud IM application](https://console.rongcloud.cn/agile/apps/km).
Leave base URL, timeout, and response size blank for their defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/user-online-status
```

In Dex Web **Start Flow**, choose `RongCloudUserOnlineStatus`, provide a unique
Flow ID, and supply an opaque IM user ID:

```json
{"userId":"user123"}
```

The output contains only `isOnline`. The mock-backed integration test uses a
real Dex Worker and Client without RongCloud credentials. Mock data is a
sample and does not reflect a live user's presence.
