# Qiniu bucket list Flow

This runnable Flow uses the static `qiniu` connection to list bucket names owned by the signing account. The `listed` branch stores the result in a Dex Attribute and completes with the names. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/qiniu/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/bucket-list/flow/workflow.go --schema-version 2.0 --json --out ./build/bucket-list
```

The output must report `valid: true`. An unpublished module may require its local release metadata to resolve the connector contract.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override qiniu=<artifact-directory>`. In Dex Web **Connections**, create the `qiniu` connection with a matched key pair from [Qiniu Key Management](https://portal.qiniu.com/user/key). Leave base URL, timeout, and response-size fields blank for their defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/bucket-list
```

In Dex Web **Start Flow**, choose `QiniuBucketList`, enter a unique Flow ID, and use `{}` as input. The output contains `buckets`, which can be empty. The mock-backed integration test uses a real Dex Worker and Client without a Qiniu credential.
