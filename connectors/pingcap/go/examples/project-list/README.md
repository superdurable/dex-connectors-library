# TiDB Cloud project list Flow

This runnable Flow uses the static `pingcap` connection to list one page of accessible projects. The `listed` branch stores the result in a Dex Attribute and completes with it. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/pingcap/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/project-list/flow/workflow.go --schema-version 2.0 --json --out ./build/project-list
```

The output must report `valid: true`. An unpublished module may require its local release metadata to resolve the connector contract.

## Run with Dex Web

Build the connector release artifact and pass it to `dexcli dev --connector-release-override pingcap=<artifact-directory>`. In Dex Web **Connections**, create the `pingcap` connection with an API key from [TiDB Cloud](https://tidbcloud.com/) **Organization Settings > API Keys**. Leave base URL, timeout, and response-size fields blank for their defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/project-list
```

In Dex Web **Start Flow**, choose `PingCAPProjectList`, enter a unique Flow ID, and use input such as `{"page":1,"pageSize":10}`. The output contains project `items` and `total`. The mock-backed integration test uses a real Dex Worker and Client without a TiDB Cloud API key.
