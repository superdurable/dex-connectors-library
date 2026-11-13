# Kingsoft Cloud IAM user list Flow

This runnable Flow uses the static `kingsoftcloud` connection to list one IAM user page. The `listed` branch stores the result in a Dex Attribute and completes with it. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/kingsoftcloud/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/user-list/flow/workflow.go --schema-version 2.0 --json --out ./build/user-list
```

The output must report `valid: true`. An unpublished module may require its local release metadata to resolve the connector contract.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override kingsoftcloud=<artifact-directory>`. In Dex Web **Connections**, create the `kingsoftcloud` connection. Follow the authorization guide to create an IAM subuser AccessKeyID and SecretAccessKey with `ListUsers` permission. Leave the displayed base URL, timeout, response size, region, and service defaults unless a trusted deployment requires an override.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/user-list
```

In Dex Web **Start Flow**, choose `KingsoftCloudUserList` and enter a unique Flow ID:

```json
{"maxItems":100}
```

The output contains `users`, `hasMore`, and `nextMarker`. To fetch the next page, start another Flow with the returned marker, for example `{"maxItems":100,"marker":"<nextMarker>"}`. The mock-backed integration test uses a real Dex Worker and Client with synthetic sample data, without a provider credential.
