# Enlvon policy catalog Flow

This runnable Flow uses static connection `enlvon` and reads the electricity policy corpus. The `found` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/enlvon/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/policy-directory/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/enlvon-policy-directory
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override enlvon=<artifact-directory>`. In Dex Web Connections, create `enlvon` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/policy-directory` and start `EnlvonPolicyCatalog` with `{"query":"分时电价","topK":1}`.

For the local synthetic mock, run `dexcli dev -dex-port 10421 -web-port 10422 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10421 GOWORK=off go test -tags integration ./examples/policy-directory/flow -run TestPolicyCatalogUsesDefaultMockWithRealDex -count=1`.
