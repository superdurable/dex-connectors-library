# XiaoeTech root goods categories Flow

This runnable Flow uses the static `xiaoetech` connection to list root goods categories. The `listed` branch stores IDs, titles, and child availability in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

Validate its strict FDG 2.0 source from this Go module:

```sh
dexcli visualize ./examples/root-goods-categories/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/xiaoetech-root-categories
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override xiaoetech=<artifact-directory>`. In Dex Web Connections, create `xiaoetech` using the [connection guide](../../../README.md#connection). With the project's `DEX_PROJECT_*` environment set, run `GOWORK=off go run ./examples/root-goods-categories`. Start `XiaoeTechRootCategories` with empty input `{}`. The output is a bounded list of root categories.

For local mock integration, run `dexcli dev -dex-port 10011 -web-port 10012 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10011 GOWORK=off go test -tags integration ./examples/root-goods-categories/flow`.
