# STO Waybill Trace Flow

This runnable Worker registers `STOWaybillTrace` with static ConnectionName `sto`. It executes the provider Query once, stores the minimized tracking result in an Attribute, and completes with the same typed result. Only the happy-path branch is wired.

From `connectors/sto/go`, generate and inspect the strict FDG 2.0 graph:

```bash
dexcli visualize ./examples/waybill-trace/flow/workflow.go --schema-version 2.0 --json --out /tmp/sto-waybill-trace-fdg
```

Build a connector release artifact, then start `dexcli dev` with `--connector-release-override sto=<artifact-directory>`. In Dex Web Connections select `sto`, follow the [connection guide](../../../README.md), and enter App Key, secret App Secret, and application resource code. Leave other settings blank for displayed defaults. Run the Worker:

```bash
GOWORK=off go run ./examples/waybill-trace
```

Start `STOWaybillTrace` with a unique Flow ID and `{"waybillNo":"884000400421364"}` for an authorized waybill. The Worker connects to Dex Server without an inbound listener. The integration test uses a real Dex Worker and Client with the generated provider mock. Live STO authorization and tracking data remain unverified.
