# Rainbond team catalog Flow

This runnable Flow uses static connection `rainbond` and reads one team page. The `listed` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/rainbond/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/team-catalog/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/rainbond-team-catalog
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override rainbond=<artifact-directory>`. In Dex Web Connections, create `rainbond` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/team-catalog` and start `RainbondTeamCatalog` with `{}`.

For the local synthetic mock, run `dexcli dev -dex-port 10241 -web-port 10242 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10241 GOWORK=off go test -tags integration ./examples/team-catalog/flow -run TestTeamCatalogUsesDefaultMockWithRealDex -count=1`.
