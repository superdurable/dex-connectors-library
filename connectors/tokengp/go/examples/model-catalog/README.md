# TokenGP model catalog Flow

This runnable Flow uses the static `tokengp` connection to list model IDs accessible to its API key. The `listed` branch stores the result in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

Validate its strict FDG 2.0 source from this Go module:

```sh
dexcli visualize ./examples/model-catalog/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/tokengp-model-catalog
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override tokengp=<artifact-directory>`. In Dex Web Connections, create `tokengp` using the [connection guide](../../../README.md#connection). With the project's `DEX_PROJECT_*` environment set, run `GOWORK=off go run ./examples/model-catalog`. Start `TokenGPModelCatalog` with empty input `{}`. The output contains `models` with model IDs.

For local mock integration, run `dexcli dev -dex-port 10011 -web-port 10012 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10011 GOWORK=off go test -tags integration ./examples/model-catalog/flow`.
