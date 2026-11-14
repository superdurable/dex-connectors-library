# UCloud region list Flow

This runnable Flow uses the static `ucloud` connection to list the account's accessible regions and zones. The `found` branch stores the result in a Dex Attribute and completes with it. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/ucloud/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/region-list/flow/workflow.go --schema-version 2.0 --json --out ./build/region-list
```

The output must report `valid: true`. An unpublished module may require its local release metadata to resolve the connector contract.

## Run with Dex Web

Build the connector release artifact and pass it to `dexcli dev --connector-release-override ucloud=<artifact-directory>`. In Dex Web **Connections**, create the `ucloud` connection using a matched key pair from [UCloud API Keys](https://console.ucloud.cn/uaccount/api_manage). Leave base URL, timeout, and response-size fields blank for their defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/region-list
```

In Dex Web **Start Flow**, choose `UCloudRegionList`, enter a unique Flow ID, and use `{}` as input. The output contains `regions`, which can be empty. The mock-backed integration test uses a real Dex Worker and Client without UCloud credentials.
