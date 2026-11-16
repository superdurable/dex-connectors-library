# Re:amaze connector for Python

`dex-connector-reamaze` provides six async Re:amaze operations for Dex Flows: search and read conversations, create and update conversations, add a reply or internal note, and find a contact by email. It also provides the `conversationCreated` poll Trigger. It uses the `dex_connectors.reamaze` import package, requires Python 3.11 or newer, and depends on `dex-connectors-sdk>=0.12.0`, which adds the poll Trigger kit.

Install `dex-connector-reamaze==0.2.0` after release. The
[contact lookup example](examples/find_contact/README.md) contains the Worker,
Client start command, and a real Dex integration test.

Configure `brand` as the lowercase subdomain in `https://{brand}.reamaze.io`; set `email` to the staff login address and store the user's API token in the secret `api_token` credential field. Re:amaze calls use HTTP Basic on the brand's `/api/v1` endpoint. Each call reads current credentials, and the token is kept out of Flow inputs and results.

The write operations preserve Go's branch identities and Dex retry behavior. `createConversation` and `replyToConversation` use a single-dispatch heartbeat fence and read-back to avoid resending a request whose outcome is unknown. `updateConversation` reads the conversation before setting absolute status, assignee, and tag values. An unwired optional branch fails the Flow.

For `findContactByEmail`, `notFound` means a successful contact search returned no exact email match. A 404 response from `/contacts` selects `providerRejected`.

## conversationCreated Trigger

`conversationCreated` delivers one `ConversationCreatedEvent` per conversation created in the connection's brand, or in the channel a binding saves, with its first message. Each poll sends `GET /conversations?filter=all`, plus `category=<channel>` for a saved channel, so archived and spam conversations arrive too; an application filters on `conversation.status`. The behavior matches the Go connector, which the [connector README](../README.md#conversationcreated-trigger) describes in full: the first poll reads the newest page and delivers nothing, the cursor keeps the newest creation time, a floor 10 minutes behind it, and at most 800 slug digests seen at or above the floor, the walk detects the listing order and reads at most 16 pages of 30, a larger burst delivers the newest 480 and logs a WARN record, a cursor for another brand or channel or one that does not decode reseeds, and a 429 pauses the connection for `Retry-After`, or one minute without it. The cursor token is byte-compatible with Go, and event IDs, times, and inbox payload values match, so Go and Python replicas can share one binding; `tests/test_conversation_created_go_parity.py` replays a Go recording to prove it.

The connection's `pollInterval` setting, `Config.poll_interval_seconds` in code, is the delay between polls; `Client` rejects one under 10 seconds. Re:amaze's per-minute token limit is shared with operation Steps.

| Function | Use |
| --- | --- |
| `new_project_conversation_trigger_runner(project, connection, config)` | Durable delivery from a `LoadedProject`: each route's saved channel is loaded (an unsaved binding watches every channel), and each conversation is recorded in the binding's project Trigger inbox before its cursor commits. |
| `new_conversation_trigger_runner(config)` | One `PollRunner` for `ConversationCreatedTriggerRoute` values with your own targets and cursor stores; a route without a cursor keeps it in memory. |
| `new_conversation_created_trigger(connection=, connection_name=, target=)` | One in-memory `TriggerRunner`, like Go's generated `NewConversationCreatedTrigger`; it records a new baseline on every start. |
| `define_conversation_created_trigger_binding(connection_name=, binding_name=, configuration_ui=)` | Declares the binding in a Flow's `get_connector_trigger_bindings`, with the `channelSlugEntry` unit (`UI_UNIT_CHANNEL_SLUG_ENTRY`, port `UI_CHANNEL_SLUG_ENTRY_PORT_CHANNEL`) bound to `/channel`. |

The [`new_conversation` example](examples/new_conversation/README.md) wires a Flow target in [`worker.py`](examples/new_conversation/worker.py):

```python
route = ProjectConversationCreatedTriggerRoute(BINDING_NAME, new_conversation_flow_target(dex_client, flow))
runner = new_project_conversation_trigger_runner(
    project, connection, ProjectConversationTriggerRunnerConfig([route])
)
```

`PollRunner.run(TriggerContext(cancellation))` replays the inboxes, seeds, and polls until the cancellation fires, then raises `TriggerDeliveryAbortedError`.

## Generated code and verification

The generated `dex-connector.json`, models, connection bindings, Step factories, and mocks come from `connector.yaml`. The provider client and operation invocations are hand-written. Provider replay uses the Go-recorded conformance scenarios. No live Re:amaze authorization, polling, or provider request has been verified for this Python package.
