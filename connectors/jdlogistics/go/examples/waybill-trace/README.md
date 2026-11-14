# JD Logistics Waybill Trace Flow

This runnable Worker registers `JDLogisticsWaybillTrace` with static ConnectionName `jdlogistics`. It executes the provider Query once, stores the minimized tracking result in an Attribute, and completes with the same typed result. Only the happy-path branch is wired.

From `connectors/jdlogistics/go`, generate and inspect the strict FDG 2.0 graph:

```bash
dexcli visualize ./examples/waybill-trace/flow/workflow.go --schema-version 2.0 --json --out /tmp/jdlogistics-waybill-trace-fdg
```

Build a connector release artifact, then start `dexcli dev` with `--connector-release-override jdlogistics=<artifact-directory>`. In Dex Web Connections select `jdlogistics`, follow the [connection guide](../../../README.md), and enter AppKey, secret AppSecret, secret access token, and merchant customer code. Leave other settings blank for displayed defaults. Run the Worker:

```bash
GOWORK=off go run ./examples/waybill-trace
```

Start `JDLogisticsWaybillTrace` with a unique Flow ID and `{"waybillNo":"JDX009866825381"}` for an authorized B2C merchant waybill. The integration test uses a real Dex Worker and Client with the generated provider mock. Live JD Logistics authorization and tracking data remain unverified.
