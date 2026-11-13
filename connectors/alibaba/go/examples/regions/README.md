# Alibaba Cloud ECS regions Flow

This runnable Flow uses the static `alibaba` connection to call `DescribeRegions` once. The `fetched` branch stores region IDs, local names, endpoints, and sale status in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

Run from the Go connector module:

```sh
go run ./examples/regions
```

In Dex Web **Connections**, create the `alibaba` connection. Sign in at the [RAM console](https://ram.console.aliyun.com), open **Identities > Users**, select a RAM user, then open **Credential** (or **Authentication**) > **AccessKey** and create a key pair. Copy its AccessKey ID and one-time AccessKey secret into the two secret fields. Leave the ECS endpoint, timeout, and response size blank for their displayed defaults.

For local testing, build the connector release artifact and pass it to `dexcli dev --connector-release-override alibaba=<artifact-directory>`. The `sampleRegions` mock returns synthetic data.

In Dex Web **Start Flow**, choose `AlibabaCloudRegions`, supply a unique Flow ID, and enter:

```json
{}
```

Generate the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/regions/flow/workflow.go --schema-version 2.0 --json --out /tmp/alibaba-regions-fdg
```
