# Tianxia Gongchang account status Flow

This runnable Flow uses static connection `tianxiagongchang` and reads the free account endpoint. The `retrieved` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/tianxiagongchang/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/account-status/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/tianxiagongchang-account-status
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override tianxiagongchang=<artifact-directory>`. In Dex Web Connections, create `tianxiagongchang` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/account-status` and start `TianxiagongchangAccountStatus` with `{}`.

For the local synthetic mock, run `dexcli dev -dex-port 10271 -web-port 10272 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10271 GOWORK=off go test -tags integration ./examples/account-status/flow -run TestAccountStatusUsesDefaultMockWithRealDex -count=1`.
