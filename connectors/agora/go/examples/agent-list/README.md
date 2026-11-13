# Agora running agents Flow

This runnable Flow calls `listRunningAgents` through the static `agora` connection, persists the result, and completes with typed output. Optional branches fail the Flow when selected.

From `connectors/agora/go`, generate the strict Flow Definition Graph 2.0 artifact:

```bash
dexcli visualize ./examples/agent-list/flow/workflow.go --schema-version 2.0 --json --out /tmp/agora-agent-list-fdg
```

Start a Dex development server in another terminal and configure the `agora` connection in Dex Web. Then run:

```bash
GOWORK=off go run ./examples/agent-list
```

Start `AgoraAgentList` with a unique Flow ID and empty input `{}`. The Worker connects to Dex Server without an inbound listener. The `integration` tagged test runs the generated mock against a real Dex server. Live Agora authorization and agent discovery remain unverified.
