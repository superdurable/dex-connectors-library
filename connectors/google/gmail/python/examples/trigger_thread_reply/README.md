# Gmail Trigger thread reply example for Python

This example mirrors the Go [`thread-reply`](../../../go/examples/thread-reply/README.md)
example:

1. a matching received root message starts a `GmailThreadReplyPython` Flow
   through the `messageReceived` Trigger;
2. the Flow reads that message with `getMessage`;
3. a matching received reply invokes the typed `receive_email_reply` RPC
   through the `replyReceived` Trigger; and
4. the Flow replies `Processing complete.` in the same Gmail thread with
   `replyToMessage`.

Both Triggers build the same Flow ID from the connection name and the Gmail
thread ID. The Flow start uses the event ID as its request ID, so a redelivered
root finds its Flow. The RPC locks the thread state and stores the accepted
reply event ID, so a redelivered reply cannot schedule a second reply. A reply
whose Flow never started, or whose Flow already completed, is consumed as
undeliverable and logged once. A reply that arrives while the Flow still reads
its root makes the RPC raise, so the reply stays pending and is retried on the
next poll.

[`app.py`](app.py) runs both bindings, `gmail-thread-reply-start` and
`gmail-thread-reply-received`, on one `new_project_message_trigger_runner` for
the `gmail-inbox` connection. Each poll lists replies before roots and then
delivers every root before any reply. Before either Dex call, each target
applies a filter built from the binding configuration saved by Dex Web, which
checks the root or reply shape, the sender allowlist, and the subject or
snippet substring again.

Unlike the Go and TypeScript examples, the completion reply text is fixed in
[`flow.py`](flow.py): the Python `new_reply_to_message_step` factory has no
Step configuration UI, so Dex Web cannot save one for it.

## Configure

Follow the [package setup](../../README.md#configure-a-connection), then
configure the `gmail-inbox` connection and both Trigger bindings in Dex Web.
Each binding shows three optional fields:

- **Gmail search query**: Gmail search syntax, such as `label:inbox is:unread`,
  as typed in the Gmail search box; blank inspects the most recent inbox
  messages.
- **Start message contains** or **Reply message contains**: plain text that
  must appear in the subject or snippet, ignoring case; blank matches any text.
- **Allowed root senders** or **Allowed reply senders**: complete sender email
  addresses; blank allows any sender the query matches.

The connection's **Poll interval** and **Poll page size** set how often the
Triggers poll and how many recent inbox messages each poll inspects. The Worker
reads the saved values once at startup, so restart it after saving.

## Run

From `connectors/google/gmail/python`, generate strict FDG 2.0 and start
`dexcli dev` with it:

```bash
mkdir -p build
uv run dexcli visualize ./examples/trigger_thread_reply/flow.py --schema-version 2.0 --json --out ./build/trigger_thread_reply
dexcli dev -flow-rendering-dir ./build
```

Dex CLI 1.47.3 reports `connector_configuration_ui` warnings for the Python
binding configuration units and renders the bindings without them, as for the
other Python Trigger examples; the graph is otherwise valid and deterministic.

Then run the Worker, which also runs both Triggers, with the `connections.json`
that Dex Web wrote:

```bash
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.trigger_thread_reply.main
```

Set `LOG_LEVEL=debug` to also see every message the Triggers ignore, with the
reason, and every delivery to Dex. Trigger records go to the
`dex_connectors.sdk.trigger` logger and carry message and thread IDs, never
senders, subjects, snippets, bodies, or tokens.

## Test

```bash
uv run pytest tests/test_trigger_thread_reply_example.py
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/trigger_thread_reply/integration_test.py
```

The integration tests run this application against a real Dex server with a
local fake of the Gmail API. They cover an ignored root, an orphan reply, a
non-matching reply, the completion reply sent once in the thread, a reply
retried while its Flow still reads the root, and a restart that rescans the
page without starting a second Flow or sending a second reply. Live Gmail is
not verified.
