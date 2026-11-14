# Volcengine ECS region list Flow

This runnable Flow uses the static `volcengine` connection to list one ECS region page. The `listed` branch stores the provider result in a Dex Attribute and completes with it. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/volcengine/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/region-list/flow/workflow.go --schema-version 2.0 --json --out ./build/region-list
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override volcengine=<artifact-directory>`. In Dex Web **Connections**, create the `volcengine` connection. At [Volcengine Console](https://console.volcengine.com), open 访问控制 > 用户管理, select a dedicated IAM user with ECS DescribeRegions permission, then create an Access Key in its 密钥 tab. Enter the Access Key ID and Secret Access Key as secret credentials. Leave the displayed endpoint, timeout, response size, region, and service defaults unless you use an approved regional endpoint.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/region-list
```

In Dex Web **Start Flow**, choose `VolcengineRegionList` and enter a unique Flow ID:

```json
{"maxResults":10}
```

The output contains `regionIds` and `nextToken`. If `nextToken` is nonempty, pass it as the next Flow's input, for example `{"maxResults":10,"nextToken":"<nextToken>"}`. The mock-backed integration test uses a real Dex Worker and Client with synthetic sample data and no provider credential.
