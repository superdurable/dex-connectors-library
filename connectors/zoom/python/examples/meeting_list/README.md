# Zoom meeting list example

This Flow lists one page of the authorized user's Zoom meetings through the static Dex connection name `zoom-meetings`. It leaves optional failure branches unwired, so a rejection fails the Flow.

Generate a strict Flow Definition Graph 2.0 from `connectors/zoom`:

```bash
mkdir -p python/build
dexcli visualize ./python/examples/meeting_list/flow.py --schema-version 2.0 --json --out ./python/build/meeting_list
```

Configure a Zoom OAuth connection named `zoom-meetings` in Dex Web using the authorization guide in `../connector.yaml`. Start `dexcli dev` with available local ports, then run these from `connectors/zoom/python`:

```bash
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=localhost:<dex-port> uv run python -m examples.meeting_list.worker
DEX_FLOW_SERVICE_ADDRESS=localhost:<dex-port> uv run python -m examples.meeting_list.start <unique-flow-id> --page-size 30
```

The checked-in integration test exercises this Flow against a real Dex Worker and Client with a fake Zoom provider. Live Zoom OAuth and provider behavior remain unverified until an author uses their own Dex Web connection.
