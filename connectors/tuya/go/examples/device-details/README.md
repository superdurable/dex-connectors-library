# Tuya device details Flow

Run the Worker from this module after starting `dexcli dev`:

```sh
GOWORK=off go run ./examples/device-details
```

Create a Dex Web connection named `tuya` using the [connection guide](../../../README.md#connection), then start the `TuyaDeviceDetails` Flow with input such as `{"deviceId":"60613135b121cddc294a"}`. The `found` result contains only non-secret metadata. The device's `local_key`, refresh/access tokens, and status values are never persisted.

Verify the default mock with a real Dex Server:

```sh
dexcli dev -dex-port 9051 -web-port 9052 -open=false
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9051 GOWORK=off go test -tags integration ./examples/device-details/flow -run TestDeviceDetailsUsesDefaultMockWithRealDex
```

Validate the Flow Definition Graph 2.0:

```sh
dexcli visualize ./examples/device-details/flow/workflow.go --schema-version 2.0 --json --out /tmp/tuya-device-details
```
