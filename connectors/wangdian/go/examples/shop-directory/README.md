# Wangdian shop catalog Flow

This runnable Flow uses static connection `wangdian` and reads the accessible shop list. The `listed` branch stores the output in a Dex Attribute and completes. Optional failure branches are unwired and fail the Flow.

From `connectors/wangdian/go`, generate and lint the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/shop-directory/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/wangdian-shop-directory
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override wangdian=<artifact-directory>`. In Dex Web Connections, create `wangdian` using the [connection guide](../../../README.md#connection). With `DEX_PROJECT_*` configured, run `GOWORK=off go run ./examples/shop-directory` and start `WangdianShopCatalog` with `{}`.

For the local synthetic mock, run `dexcli dev -dex-port 10401 -web-port 10402 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10401 GOWORK=off go test -tags integration ./examples/shop-directory/flow -run TestShopCatalogUsesDefaultMockWithRealDex -count=1`.
