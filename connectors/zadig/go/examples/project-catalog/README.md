# Zadig project catalog Flow

This runnable Flow uses the static `zadig` connection to read one page of projects. The `listed` branch stores project keys, names, deployment types, and the total count in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

Validate its strict FDG 2.0 source from this Go module:

```sh
dexcli visualize ./examples/project-catalog/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/zadig-project-catalog
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override zadig=<artifact-directory>`. In Dex Web Connections, create `zadig` using the [connection guide](../../../README.md#connection). With the project's `DEX_PROJECT_*` environment set, run `GOWORK=off go run ./examples/project-catalog`. Start `ZadigProjectCatalog` with input `{}` for page 1 and size 20, or supply `pageNum` and `pageSize` (1 to 100). The output contains project metadata and the total count.

For local mock integration, run `dexcli dev -dex-port 10221 -web-port 10222 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10221 GOWORK=off go test -tags integration ./examples/project-catalog/flow`.
