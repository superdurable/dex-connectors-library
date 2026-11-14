# Qiyuesuo contract status Flow

This runnable Flow uses static connection `qiyuesuo` and reads one contract page. The `listed` branch stores IDs and statuses in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/qiyuesuo/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/contract-status/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/qiyuesuo-contract-status
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override qiyuesuo=<artifact-directory>`. In Dex Web Connections, create `qiyuesuo` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/contract-status` and start `QiyuesuoContractStatus` with `{}`.

For the local synthetic mock, run `dexcli dev -dex-port 10321 -web-port 10322 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10321 GOWORK=off go test -tags integration ./examples/contract-status/flow -run TestContractStatusUsesDefaultMockWithRealDex -count=1`.
