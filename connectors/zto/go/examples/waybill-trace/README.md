# ZTO Waybill Trace Flow

This runnable Worker registers `ZTOWaybillTrace` with static ConnectionName `zto`. It executes the provider Query once, stores the minimized tracking result in an Attribute, and completes with the same typed result. Only the happy-path branch is wired.

From `connectors/zto/go`, generate and inspect the strict FDG 2.0 graph:

```bash
dexcli visualize ./examples/waybill-trace/flow/workflow.go --schema-version 2.0 --json --out /tmp/zto-waybill-trace-fdg
```

Build a connector release artifact, then start `dexcli dev` with `--connector-release-override zto=<artifact-directory>`. In Dex Web Connections select `zto`, follow the [connection guide](../../../README.md), and enter AppKey plus secret AppSecret. Leave other settings blank for displayed defaults. Run the Worker:

```bash
GOWORK=off go run ./examples/waybill-trace
```

Start `ZTOWaybillTrace` with a unique Flow ID and `{"billCode":"73100059800035"}` for an authorized waybill. The Worker connects to Dex Server without an inbound listener. The integration test uses a real Dex Worker and Client with the generated provider mock. Live ZTO authorization and tracking data remain unverified.
