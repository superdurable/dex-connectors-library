# Gmail thread reply example

This example keeps one complete inbound Gmail integration beside the Gmail
Connector:

1. a matching received root message starts a Flow;
2. the Flow reads that message with `GetMessage`;
3. a matching received reply invokes the typed `ReceiveEmailReply` RPC; and
4. the Flow replies `Processing complete.` in the same Gmail thread with `ReplyToMessage`.

Both neutral provider Triggers use the same application resolver to map the
connection name and Gmail thread ID to a stable, PII-safe Flow ID. The
application chooses a Flow-start target for `messageReceived` and an RPC target
for `replyReceived`; those choices are not encoded in the Connector manifest.

The reply Mutation configures the optional `ResultAttribute`. The Flow
registers that typed Attribute explicitly, and its summary/display RPCs read the
full provider result outside the transition chain for operator inspection. This
intentionally duplicates the durable target input; applications should omit the
Attribute when no external reader needs the raw result.

Before either Dex call, the application supplies a typed filter. The example
builds those filters from the binding configuration saved by Dex Web and checks
the root-or-reply shape, sender address, and case-insensitive subject/snippet
substring again. A filtered event is consumed without resolving a Flow ID or
calling Dex. The pure filter has no error result.

## Configure

Follow the [Gmail Connector setup](../../README.md), then configure the
`gmail-inbox` connection and these Trigger bindings in Dex Web:

- `gmail-thread-reply-start` for `messageReceived`;
- `gmail-thread-reply-received` for `replyReceived`.

Both bindings accept an optional Gmail `searchQuery`, case-insensitive
`messageContains`, and sender allowlist. The example uses the Gmail thread ID
as its durable identity.

The alpha local Trigger transport polls the newest inbox messages. It derives
each event ID from the immutable Gmail message ID. The example runs both
bindings through one `NewLocalMessageTriggerRunner`. Each poll lists replies
before roots and then delivers every root before any reply, so a root always
starts its Flow before its reply arrives. A process restart may rescan the
current page, but deterministic Flow starts and the application RPC's bounded
accepted reply event ID absorb duplicate delivery.

The example writes text logs to standard error at INFO. Set `LOG_LEVEL=debug`
to also see every message a Trigger ignores, with the reason, and every
delivery to Dex. The logs contain message and thread IDs and error messages,
never senders, subjects, snippets, bodies, or tokens.

A reply to a completed thread, including one found again by a restart rescan,
is consumed as undeliverable and logged once:

```text
time=2026-09-25T22:41:07.412-07:00 level=WARN msg="trigger event skipped: undeliverable" connector=gmail connection=gmail-inbox trigger=replyReceived binding=gmail-thread-reply-received thread_id=18c2f0a1b2c3d4e0 event_id=18c2f0a1b2c3d4e5 flow_id=gmail-thread-reply-gmail-inbox-18c2f0a1b2c3d4e0 error="Trigger event is undeliverable: dex: InvokeRPC flow \"gmail-thread-reply-gmail-inbox-18c2f0a1b2c3d4e0\": NotFound: workflow execution already completed"
```

A reply that arrives while the Flow is still reading its root message stays
pending and is retried on the next poll. Until that Flow finishes reading its
root or closes, the reply poll stops at that reply, so later replies for every
thread wait too. The read Step's retry policy bounds this delay. Other
failures, such as a Dex Server that becomes unreachable, are retried on the
next poll the same way, even after newer mail pushes the message off the polled
page. Each attempt logs its number and the poll interval as its delay, and the
recovery follows:

```text
time=2026-09-25T22:43:10.101-07:00 level=WARN msg="trigger delivery failed; retrying" connector=gmail connection=gmail-inbox trigger=replyReceived binding=gmail-thread-reply-received thread_id=18c2f0a1b2c3d4e1 event_id=18c2f0a1b2c3d4e6 attempt=1 delay=10s flow_id=gmail-thread-reply-gmail-inbox-18c2f0a1b2c3d4e1 error="dex: InvokeRPC flow \"gmail-thread-reply-gmail-inbox-18c2f0a1b2c3d4e1\": FailedPrecondition: Gmail thread is still reading its root message"
time=2026-09-25T22:43:20.130-07:00 level=INFO msg="trigger delivered after retry" connector=gmail connection=gmail-inbox trigger=replyReceived binding=gmail-thread-reply-received thread_id=18c2f0a1b2c3d4e1 event_id=18c2f0a1b2c3d4e6 attempts=2
```

If the Flow completes while its reply is retrying, the next poll consumes the
reply as undeliverable and logs only the skip, not a recovery. A failed list
or message read logs `gmail poll failed; retrying` with the same delay and an
`attempt` that counts the failed polls in a row. Production push delivery and
durable Gmail history checkpoints are outside this local example.

## Run

Before starting Dex, verify the example with the dexcli release pinned in the
repository's `.dex-compat-version` file:

```bash
mkdir -p build
dexcli visualize ./examples/thread-reply/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/gmail-thread-reply
```

The compatibility gate runs this command twice from a clean consumer module.
The schema must be valid, deterministic, and include both Gmail connector
Steps, both Trigger bindings, branch targets, and Result Attributes.

Start Dex, then run the example with the connection file shown by Dex Web:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/thread-reply
```

The default Worker address is `127.0.0.1:8814`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or
`DEX_BLOB_CACHE_DIR` when needed. If the Dex Server is unreachable when the
example starts, it logs `dex server unavailable; retrying` with a `delay` that
grows to 30 seconds, and starts the Worker and the Gmail runner once Dex
answers.

The example wires only the happy-path branches. An unwired optional branch fails
the Flow. An uncertain reply is not resent.
