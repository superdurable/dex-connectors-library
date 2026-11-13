# JINGdigital tag catalog Flow

This runnable Flow uses static connection `jingdigital` and reads the accessible tag list. The `listed` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/jingdigital/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/tag-directory/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/jingdigital-tag-directory
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override jingdigital=<artifact-directory>`. In Dex Web Connections, create `jingdigital` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/tag-directory` and start `JINGdigitalTagCatalog` with `{}`.

For the local synthetic mock, run `dexcli dev -dex-port 10391 -web-port 10392 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10391 GOWORK=off go test -tags integration ./examples/tag-directory/flow -run TestTagCatalogUsesDefaultMockWithRealDex -count=1`.
