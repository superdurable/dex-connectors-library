# West.cn domain catalog Flow

This runnable Flow uses the static `west` connection to read one page of domains. The `listed` branch stores names, expiration dates, and pagination counts in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

Validate its strict FDG 2.0 source from this Go module:

```sh
dexcli visualize ./examples/domain-catalog/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/west-domain-catalog
```

Build the connector release artifact and pass it to `dexcli dev --connector-release-override west=<artifact-directory>`. In Dex Web Connections, create `west` using the [connection guide](../../../README.md#connection). With the project's `DEX_PROJECT_*` environment set, run `GOWORK=off go run ./examples/domain-catalog`. Start `WestDomainCatalog` with input `{}` for page 1 and limit 20, or supply `page` and `limit` (1 to 100). The output contains domain names, expiration dates, and provider pagination counts.

For local mock integration, run `dexcli dev -dex-port 10211 -web-port 10212 -open=false`, then `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10211 GOWORK=off go test -tags integration ./examples/domain-catalog/flow`.
