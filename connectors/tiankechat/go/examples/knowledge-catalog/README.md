# TiankeChat knowledge catalog Flow

This runnable Flow uses the static `tiankechat` connection to list one page of knowledge entry metadata. The `listed` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired, so they fail the Flow when selected.

From `connectors/tiankechat/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/knowledge-catalog/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/tiankechat-knowledge-catalog
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override tiankechat=<artifact-directory>`. In Dex Web Connections, create `tiankechat` using the [connection guide](../../../README.md#connection). With the project's `DEX_PROJECT_*` environment set, run `GOWORK=off go run ./examples/knowledge-catalog`. Start `TiankeChatKnowledgeCatalog` with `{}`. The output contains `entries`, `total`, `page`, and `limit`, without question or answer text.

For local mock integration, run `dexcli dev -dex-port 10231 -web-port 10232 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10231 GOWORK=off go test -tags integration ./examples/knowledge-catalog/flow -run TestKnowledgeCatalogUsesDefaultMockWithRealDex -count=1`. The mock data is synthetic and is not read from a tenant.
