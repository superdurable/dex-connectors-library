# OneNET device detail Flow

This runnable Flow calls `queryDeviceDetail` through the static `onenet` connection, persists the sanitized result, and completes with typed output. Optional branches fail the Flow when selected.

From `connectors/onenet/go`, generate the strict Flow Definition Graph 2.0 artifact:

```bash
dexcli visualize ./examples/device-read/flow/workflow.go --schema-version 2.0 --json --out /tmp/onenet-device-read-fdg
```

Start a Dex development server in another terminal and configure the `onenet` connection in Dex Web. Then run:

```bash
GOWORK=off go run ./examples/device-read
```

Start `OneNETDeviceRead` with a unique Flow ID and `{"deviceName":"your-device-name"}`. The Worker connects to Dex Server without an inbound listener. The `integration` tagged test runs the generated mock against a real Dex server. Live OneNET authorization and device retrieval remain unverified.
