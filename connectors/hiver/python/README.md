# Hiver Python connector

Install `dex-connector-hiver==0.1.0` alongside Dex Python applications. The package implements `listInboxes`, `listConversations`, `getConversation`, `updateConversation`, `addNote`, and `createSharedDraft`; it has no Triggers.

Use the static `hiver-support` connection in the checked-in [conversation claim example](examples/claim_conversation/README.md). Its [Flow](examples/claim_conversation/flow.py) wires all six operation-specific Step factories and its [Worker](examples/claim_conversation/main.py) loads credentials from Dex project configuration. The example uses a real Dex Worker and Client in its integration test, with a local provider stand-in bound to an operating system assigned port. The smaller [inbox page example](examples/inbox_page/README.md) shows a single Query.

The client resolves `api_key` for each provider call and sends it as a bearer credential. It enforces `maxResponseBytes` on successful responses and spaces requests by `requestIntervalMilliseconds` across all operations of the same client. `updateConversation` is an absolute change followed by a read-back; `addNote` and `createSharedDraft` use Dex's single-dispatch fence because Hiver offers no idempotency key or read-back for those writes. An unconfirmed write reports `uncertain` and is not sent again.

Python's `datetime` retains microseconds. If Hiver accepts an `addNote` request but returns a `created_at` timestamp with nonzero digits beyond six fractional places, this package reports `uncertain` with a protocol failure naming `created_at`; it does not round or resend the note. RFC 3339 timestamps at microsecond precision or lower, including comma fractions and Hiver's accepted offsets, match the Go connector's UTC output.

Run `uv run pytest tests` from this directory for the Go-recorded provider replay and package checks. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dexcli-port> uv run pytest examples/claim_conversation/integration_test.py examples/inbox_page/integration_test.py` against a local `dexcli dev` process to verify the consuming Flows. Live Hiver API key authorization and provider behavior have not been verified.
