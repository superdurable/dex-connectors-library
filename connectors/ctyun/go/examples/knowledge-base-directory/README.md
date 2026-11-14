# CTYun knowledge-base catalog Flow

This runnable Flow uses static connection `ctyun` and reads the accessible knowledge-base list. The `listed` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/ctyun/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/knowledge-base-directory/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/ctyun-knowledge-base-directory
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override ctyun=<artifact-directory>`. In Dex Web Connections, create `ctyun` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/knowledge-base-directory` and start `CTYunKnowledgeBaseCatalog` with `{}`.

For the local synthetic mock, run `dexcli dev -dex-port 10431 -web-port 10432 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10431 GOWORK=off go test -tags integration ./examples/knowledge-base-directory/flow -run TestKnowledgeBaseCatalogUsesDefaultMockWithRealDex -count=1`.
