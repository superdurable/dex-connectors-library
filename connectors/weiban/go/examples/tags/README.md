# Weiban Assistant tags Flow

This runnable Flow uses the static `weiban` connection to list the first 10 tag definitions. The `listed` branch stores the typed result in a Dex Attribute and completes. Optional error branches remain unwired and fail the Flow when selected.

Generate a strict Flow Definition Graph 2.0:

```bash
cd connectors/weiban/go
mkdir -p build/tags
dexcli visualize ./examples/tags/flow/workflow.go --schema-version 2.0 --json --out ./build/tags
```

Start a Dex development server, create the `weiban` connection in Dex Web, and enter corp_id and the protected secret from Application Management > API Access. Then run:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10071 go run ./examples/tags
```

In Dex Web **Start Flow**, choose `WeibanTags`, enter a unique Flow ID, and use an empty JSON input:

```json
{}
```

The mock-backed integration test uses a real Dex Worker and Client with synthetic tags and no provider credential. It does not verify live authorization or tag data.
