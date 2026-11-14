# Gitee repository lookup Flow

This runnable Flow uses the static `gitee` connection to read metadata for one repository. The `found` branch stores the result in a Dex Attribute and completes with it. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/gitee/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/repository-lookup/flow/workflow.go --schema-version 2.0 --json --out ./build/repository-lookup
```

The output must report `valid: true`. An unpublished module may require its local release metadata to resolve the connector contract.

## Run with Dex Web

Build the connector release artifact and pass it to `dexcli dev --connector-release-override gitee=<artifact-directory>`. In Dex Web **Connections**, create the `gitee` connection using a token from [Personal Access Tokens](https://gitee.com/profile/personal_access_tokens). Leave base URL, timeout, and response-size fields blank for their defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/repository-lookup
```

In Dex Web **Start Flow**, choose `GiteeRepositoryLookup`, enter a unique Flow ID, and use input such as `{"owner":"open-source","repo":"demo"}`. The output contains `repository` metadata. The mock-backed integration test uses a real Dex Worker and Client without a Gitee credential.
