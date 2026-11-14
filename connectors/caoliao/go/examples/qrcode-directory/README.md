# Caoliao QR-code catalog Flow

This runnable Flow uses static connection `caoliao` and reads the organization QR-code directory. The `listed` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/caoliao/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/qrcode-directory/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/caoliao-qrcode-directory
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override caoliao=<artifact-directory>`. In Dex Web Connections, create `caoliao` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/qrcode-directory` and start `CaoliaoQRCodeCatalog` with `{"pageSize":20}`.

For the local synthetic mock, run `dexcli dev -dex-port 10441 -web-port 10442 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10441 GOWORK=off go test -tags integration ./examples/qrcode-directory/flow -run TestQRCodeCatalogUsesDefaultMockWithRealDex -count=1`.
