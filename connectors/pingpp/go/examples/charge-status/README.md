# Ping++ Charge status Flow

This runnable Flow uses the static `pingpp` connection to retrieve one Charge's state flags. The `fetched` branch stores the result in a Dex Attribute and completes. Optional branches are unwired and fail the Flow when selected.

Run from this Go module:

```sh
go run ./examples/charge-status
```

In Dex Web **Connections**, create the `pingpp` connection. Sign in at the [Ping++ management platform](https://dashboard.pingxx.com), click the company name, then open **企业面板 > 开发参数 > API Key**. Enter an `sk_test` key for test Charges or `sk_live` key for real Charges in the secret field. Leave the API base URL, timeout, and response size blank for their displayed defaults.

For local testing, build the connector release artifact and pass it to `dexcli dev --connector-release-override pingpp=<artifact-directory>`. The `sampleCharge` mock returns synthetic unpaid state.

In Dex Web **Start Flow**, choose `PingppChargeStatus`, supply a unique Flow ID, and enter:

```json
{"chargeId":"ch_sample123"}
```

Generate the strict FDG 2.0 graph:

```sh
dexcli visualize ./examples/charge-status/flow/workflow.go --schema-version 2.0 --json --out /tmp/pingpp-charge-status-fdg
```
