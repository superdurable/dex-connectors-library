# Hiver Python connector

Install `dex-connector-hiver==0.2.0` alongside Dex Python applications; it requires `dex-connectors-sdk` 0.12.0 or later. The package implements `listInboxes`, `listConversations`, `getConversation`, `updateConversation`, `addNote`, and `createSharedDraft`, and the `conversationReceived` poll Trigger.

Use the static `hiver-support` connection in the checked-in [conversation claim example](examples/claim_conversation/README.md). Its [Flow](examples/claim_conversation/flow.py) wires all six operation-specific Step factories and its [Worker](examples/claim_conversation/main.py) loads credentials from Dex project configuration. The example uses a real Dex Worker and Client in its integration test, with a local provider stand-in bound to an operating system assigned port. The smaller [inbox page example](examples/inbox_page/README.md) shows a single Query.

The client resolves `api_key` for each provider call and sends it as a bearer credential. It enforces `maxResponseBytes` on successful responses and spaces requests by `requestIntervalMilliseconds` across all operations of the same client. `updateConversation` is an absolute change followed by a read-back; `addNote` and `createSharedDraft` use Dex's single-dispatch fence because Hiver offers no idempotency key or read-back for those writes. An unconfirmed write reports `uncertain` and is not sent again.

Python's `datetime` retains microseconds. If Hiver accepts an `addNote` request but returns a `created_at` timestamp with nonzero digits beyond six fractional places, this package reports `uncertain` with a protocol failure naming `created_at`; it does not round or resend the note. RFC 3339 timestamps at microsecond precision or lower, including comma fractions and Hiver's accepted offsets, match the Go connector's UTC output.

## conversationReceived Trigger

Hiver documents no webhooks, so `conversationReceived` polls one picked shared inbox's conversation list at behavior parity with the Go connector. A binding's first polls walk the whole list, 10 pages of 100 per fetch and continuing at once, and record every conversation ID without delivering it. After that baseline, each poll reads the list's first page plus up to `pollPagesPerPoll` minus one pages of a sweep that walks the rest of the list across polls, and delivers each untracked ID in list order before the cursor commits. IDs that two complete sweeps in a row never list stop being tracked. A rejected page token (HTTP 400, 404, 410, or 422) restarts the walk from the first page and keeps every tracked ID. HTTP 429 pauses every binding of the connection for at least its Retry-After; other failures back off up to 30 minutes. An inbox listing more than `MAX_TRACKED_CONVERSATIONS` (50,000) pauses its binding. Each event ID is `conversation-<inboxId>-<conversationId>`, and the cursor token, tracked IDs, and their hashes match the Go connector byte for byte, so Go and Python replicas share one poll cursor document; `tests/test_conversation_received_go_parity.py` replays a recording of the Go fetcher.

`Client` reads `poll_interval_seconds` (at least one second; project configuration stores Go's `pollInterval`) and `poll_pages_per_poll` (2 to 5) from `Config`. Requests go through the client's request spacing, shared with operation Steps.

- `new_project_conversation_received_trigger_runner(project, connection, routes)` loads each binding's saved `inboxId`, keeps its cursor in project storage, and records each conversation in the binding's project Trigger inbox before the cursor commits, so a restarted replica neither misses nor replays one. A cursor for another inbox records a new baseline.
- `new_conversation_received_trigger_runner(connection, routes)` takes `ConversationReceivedTriggerRoute` values with their own cursor stores and targets.
- `new_conversation_received_trigger(...)` keeps the cursor in memory, like Go's generated factory, so each start records a new baseline.
- `define_conversation_received_trigger_binding` declares the binding; `UI_UNIT_INBOX_PICKER` and `UI_INBOX_PICKER_PORT_INBOX_ID` wire the release-owned `inboxPicker` unit.

The [new conversation intake example](examples/new_conversation_intake/README.md) starts one Flow per new conversation and reads it back with `getConversation`. Dex CLI v1.47.3 does not read Python configuration UI objects, so Dex Web shows the binding's inbox picker only for the Go example. Live Hiver polling is unverified: the list's order, Hiver's answer to an expired `next_page` token, and whether Retry-After accompanies a 429.

## Verification

Run `uv run pytest tests` from this directory for the Go-recorded provider replay and package checks. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dexcli-port> uv run pytest examples/claim_conversation/integration_test.py examples/inbox_page/integration_test.py examples/new_conversation_intake/integration_test.py` against a local `dexcli dev` process to verify the consuming Flows. Live Hiver API key authorization and provider behavior have not been verified.
