# Cloopen account balance Flow

This runnable Flow uses the static `cloopen` connection to read the main-account balance. The `fetched` branch stores the balance in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

Run from this Go module:

```sh
go run ./examples/balance
```

In Dex Web **Connections**, create the `cloopen` connection. Sign in at the [Cloopen console](https://console.yuntongxun.com) and open management console account information. Copy the 32-character **ACCOUNT SID** and **AUTH TOKEN** for the main account into the two secret fields. Leave API base URL, timeout, and response size blank for their displayed defaults.

For local testing, build the connector release artifact and pass it to `dexcli dev --connector-release-override cloopen=<artifact-directory>`. The `sampleBalance` mock returns a synthetic amount.

In Dex Web **Start Flow**, choose `CloopenBalance`, supply a unique Flow ID, and enter:

```json
{}
```

Generate the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/balance/flow/workflow.go --schema-version 2.0 --json --out /tmp/cloopen-balance-fdg
```
