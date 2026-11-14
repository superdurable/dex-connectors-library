# Wuliang AI account balance Flow

This runnable Flow uses the static `dieyuyun` connection to read the tenant's available CNY balance. The `fetched` branch stores the one-field result in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

Validate its strict FDG 2.0 source from this Go module:

```sh
dexcli visualize ./examples/account-balance/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/dieyuyun-account-balance
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override dieyuyun=<artifact-directory>`. In Dex Web Connections, create `dieyuyun` using the [connection guide](../../../README.md#connection). With the project's `DEX_PROJECT_*` environment set, run `GOWORK=off go run ./examples/account-balance`. Start `DieyuyunAccountBalance` with empty input `{}`. The output includes `availableCNY` as an exact decimal string.

For local mock integration, run `dexcli dev -dex-port 10011 -web-port 10012 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10011 GOWORK=off go test -tags integration ./examples/account-balance/flow`.
