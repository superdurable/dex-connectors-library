# Microsoft Outlook Calendar connector for Python

This package implements `listEvents`, `getEvent`, `createEvent`, `updateEvent`, and `queryFreeBusy` for Dex. Configure Microsoft OAuth or app-only access in Dex Web Connections. The generated manifest grant rotates delegated tokens or mints app-only tokens in the connection host; application code receives only the access token.

## Runnable example

`examples/read_event/` contains `ReadOutlookEventFlow`. It reads one event through the static `outlook-calendar-scheduler` connection and completes with the typed event. From this package directory:

```sh
uv sync --all-groups
dexcli visualize examples/read_event/flow.py --schema-version 2.0 --lint app --json --out /tmp/outlook-read-fdg --python .venv/bin/python
dexcli dev -dex-port 40720 -web-port 40721 -open=false
DEX_SERVER_ADDRESS=localhost:40720 uv run python -m examples.read_event.worker
```

Configure the named connection in Dex Web before running the Worker against Microsoft. Its authorization guide identifies the Entra app, Graph permission, redirect URI, tenant, and mailbox requirements. The tests use an inert local Graph response and a real Dex Worker and Client:

```sh
DEX_SERVER_ADDRESS=localhost:40720 uv run pytest examples/read_event/integration_test.py -q
```

The local conformance suite replays Go-recorded provider outcomes. Live Microsoft authorization and Graph calls require a user-controlled Dex Web connection and have not been verified by this package's tests.
