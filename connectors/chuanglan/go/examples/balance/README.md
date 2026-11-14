# Chuanglan 253 SMS balance Flow

This runnable Flow uses the static `chuanglan` connection to read remaining domestic SMS messages. The `fetched` branch stores the count in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

Run from this Go module:

```sh
go run ./examples/balance
```

In Dex Web **Connections**, create the `chuanglan` connection. Sign in at [Chuanglan SMS console](https://zz.253.com), open **产品概览 > API 接口信息**, and copy the API account and API password into the secret fields. The API account differs from your console login. If the account uses another node, enter its HTTPS origin in the base URL field; otherwise leave it blank. Leave timeout and response size blank for their displayed defaults. Add the worker's outbound IP to the account whitelist if enabled.

For local testing, build the connector release artifact and pass it to `dexcli dev --connector-release-override chuanglan=<artifact-directory>`. The `sampleBalance` mock returns a synthetic count.

In Dex Web **Start Flow**, choose `ChuanglanBalance`, supply a unique Flow ID, and enter:

```json
{}
```

Generate the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/balance/flow/workflow.go --schema-version 2.0 --json --out /tmp/chuanglan-balance-fdg
```
