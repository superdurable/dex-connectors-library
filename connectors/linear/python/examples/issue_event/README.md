# Linear Issue event Flow

This runnable Python application serves signed `issueEventReceived` webhooks
at `/linear/issues`, records each verified event in the
`linear-issue-event-python` Attribute, and completes one
`LinearIssueEventPython` Flow per action, issue UUID, and `updatedAt` time. A
redelivery finds the same Flow ID. The `issue-events` binding belongs to the
static `linear-workspace` connection. Its team picker limits recording to a
selected team; blank accepts every team included in the Linear webhook.

From `connectors/linear/python`:

```sh
uv sync --locked
dexcli visualize examples/issue_event/flow.py --language python \
  --schema-version 2.0 --lint app --json --out build/issue-event \
  --python .venv/bin/python
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
uv run --with uvicorn python -m examples.issue_event.main
```

The server listens on `LINEAR_BIND_ADDRESS` (default `127.0.0.1:8865`).
`GET /readyz` answers 200 when the binding is ready. Give Linear a public HTTPS
URL ending in `/linear/issues`. In **Settings > API > Webhooks > New webhook**,
select the **Issues** data change event, save the webhook, and copy its
signing secret from its detail page to the connection's
`webhook_signing_secret` field. The [connector README](../../../README.md#issue-webhooks)
explains OAuth application webhooks and the verification rules. An absent
secret or unavailable binding answers 503 so Linear retries; invalid signatures
and stale timestamps answer 400. A 200 means the event is durably stored before
acknowledgment.

The integration test runs this Flow and Worker against a real Dex Server. It
acknowledges a signed event while the target is unavailable, restarts the
Trigger, replays the event into a Flow, and checks that redelivery reuses the
same Flow:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
uv run pytest examples/issue_event/integration_test.py
```

Live Linear webhook delivery has not been verified.
