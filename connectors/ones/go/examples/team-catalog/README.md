# ONES team catalog Flow

This runnable Flow uses the static `ones` connection to list teams accessible to its API key. The `listed` branch stores the result in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

Validate its strict FDG 2.0 source from this Go module:

```sh
dexcli visualize ./examples/team-catalog/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/ones-team-catalog
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override ones=<artifact-directory>`. In Dex Web Connections, create `ones` using the [connection guide](../../../README.md#connection). With the project's `DEX_PROJECT_*` environment set, run `GOWORK=off go run ./examples/team-catalog`. Start `ONESTeamCatalog` with empty input `{}`. The output contains `teams` with IDs, names, and statuses.

For local mock integration, run `dexcli dev -dex-port 10111 -web-port 10112 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10111 GOWORK=off go test -tags integration ./examples/team-catalog/flow`.
