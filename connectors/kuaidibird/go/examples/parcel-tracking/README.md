# KuaidiBird parcel tracking Flow

This runnable Flow uses the static `kuaidibird` connection to query one carrier waybill. Its `retrieved` branch stores the status and provider-ordered events in a Dex Attribute and completes with the typed result. Unwired optional branches fail the Flow.

## Strict FDG 2.0 generation

From `connectors/kuaidibird/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/parcel-tracking/flow/workflow.go --schema-version 2.0 --json --out ./build/parcel-tracking
```

The output must report `valid: true`.

## Run with Dex Web

Build the connector release artifact from this source and pass it to `dexcli dev --connector-release-override kuaidibird=<artifact-directory>`. In Dex Web **Connections**, create `kuaidibird`. Open [KuaidiBird](https://www.kdniao.com), sign in to the user management console, complete account verification, and enable Instant Query. Under 我的服务 (My Services) > 用户信息 (User Information), find the decimal EBusinessID and paired API key; enter both as secret credentials. Leave the endpoint, timeout, and response-size fields blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run:

```bash
go run ./examples/parcel-tracking
```

In Dex Web **Start Flow**, choose `KuaidiBirdTracking` and enter a unique Flow ID and input:

```json
{"carrierCode":"YTO","trackingNumber":"YT1234567890123"}
```

Use a real waybill from the selected carrier and the current [carrier code table](https://www.kdniao.com/api-track). The mock-backed integration test uses a real Dex Worker and Client with synthetic events and no KuaidiBird credential. Live carrier behavior remains unverified without provider authorization.
