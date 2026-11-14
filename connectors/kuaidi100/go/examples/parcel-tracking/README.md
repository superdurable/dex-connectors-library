# Kuaidi100 parcel tracking Flow

This runnable Flow uses the static `kuaidi100` connection to read one carrier waybill. Its `retrieved` branch stores the current state and latest-first events in a Dex Attribute and completes with that typed result. Unwired optional branches fail the Flow.

## Strict FDG 2.0 generation

From `connectors/kuaidi100/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/parcel-tracking/flow/workflow.go --schema-version 2.0 --json --out ./build/parcel-tracking
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override kuaidi100=<artifact-directory>`. In Dex Web **Connections**, create the `kuaidi100` connection. Open [Kuaidi100 Enterprise](https://api.kuaidi100.com), enable or purchase Real-time Query, then find `customer` and paired `key` under 我的信息 (My Information) > 企业信息 (Enterprise Information). Enter both as secret credentials. Leave the endpoint, timeout, and response-size fields blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/parcel-tracking
```

In Dex Web **Start Flow**, choose `Kuaidi100Tracking` and enter a unique Flow ID and input:

```json
{"carrierCode":"yuantong","trackingNumber":"YT1234567890123"}
```

Use a real waybill from the selected carrier; add `phone` when that carrier requires sender or recipient verification. Avoid repeated queries for one parcel within 30 minutes. The mock-backed integration test uses a real Dex Worker and Client with synthetic sample data and no Kuaidi100 credential. Live carrier behavior remains unverified without enterprise authorization.
