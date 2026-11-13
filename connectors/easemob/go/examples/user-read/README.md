# Easemob IM user detail Flow

This runnable Flow calls `getUserDetail` through the static `easemob` connection, persists the selected user fields, and completes with typed output. Optional branches fail the Flow when selected.

From `connectors/easemob/go`, generate the strict Flow Definition Graph 2.0 artifact:

```bash
dexcli visualize ./examples/user-read/flow/workflow.go --schema-version 2.0 --json --out /tmp/easemob-user-read-fdg
```

Start a Dex development server in another terminal and configure the `easemob` connection in Dex Web. Then run:

```bash
GOWORK=off go run ./examples/user-read
```

Start `EasemobUserRead` with a unique Flow ID and `{"userId":"your-registered-user"}`. The Worker connects to Dex Server without an inbound listener. The `integration` tagged test runs the generated mock against a real Dex server. Live Easemob authorization and user retrieval remain unverified.
