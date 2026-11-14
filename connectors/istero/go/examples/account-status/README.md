# ISAS Data account status Flow

This runnable Flow uses static connection `istero` and reads the free ISAS quota endpoint. The `retrieved` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/istero/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/account-status/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/istero-account-status
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override istero=<artifact-directory>`. In Dex Web Connections, create `istero` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/account-status` and start `IsteroAccountStatus` with `{}`.

For the local synthetic mock, run `dexcli dev -dex-port 10921 -web-port 10922 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10921 GOWORK=off go test -tags integration ./examples/account-status/flow -run TestAccountStatusUsesDefaultMockWithRealDex -count=1`.
