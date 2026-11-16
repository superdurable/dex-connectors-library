# Intercom Python connector

Dex Python operations for searching and reading Intercom conversations, replying as an admin, changing conversation state, and finding contacts by email, plus the `conversationEvent` webhook Trigger. The package uses the static connection name declared in each application and resolves the current access token for each operation attempt.

```bash
pip install dex-connector-intercom==0.2.0
```

The `examples/read_conversation/` application shows a runnable Dex Flow. The `intercommock` package contains generated outcomes for deterministic application tests.

## conversationEvent Trigger

From 0.2.0 the package receives Intercom conversation webhooks at parity with the Go connector; the [connector README](../README.md#conversationevent-trigger) describes topics, retries, and ordering. Each connection's `Client` keeps one shared webhook endpoint, and every binding of the connection runs one source of it:

- `new_project_conversation_event_endpoint_runner(project, connection_name, routes)` opens the connection and every route's stored binding, wraps each route's target in the binding's durable project inbox, and returns a `ConversationEventEndpointRunner`. A notification is in project storage before Intercom receives `200` and replays after a restart. Close the runner with `aclose`.
- The runner is an ASGI 3 application: serve it with an ASGI server such as uvicorn at the URL entered in the Intercom app's **Configure > Webhooks** page, and the `lifespan` startup runs the bindings on the server's event loop. A host that owns the loop calls `run(context)` instead; `handle_request` answers one request for a host that is not an ASGI server, and `running_source_count` serves a readiness check.
- It answers `HEAD` with `200`, because Intercom validates a URL with HEAD when it is saved; `405` to a method other than `POST`; `413` to a body over `webhookMaxBodyBytes`; `503` while `client_secret` is blank or no binding runs, so Intercom retries; `400` to a bad `X-Hub-Signature` (the first header value, `sha1=` and the hex HMAC-SHA1 of the body) or an invalid envelope; and `200` to `ping` and every topic outside `conversation_event_topics()` without recording it.
- `new_conversation_event_trigger` and `new_conversation_event_webhook_handler` build one binding's runner and the connection's bare ASGI handler without project storage.

`ConversationEvent` carries `notification_id`, `topic`, `workspace_id`, `notified_at`, and the conversation without its parts or first-message body. The Trigger event ID is the notification ID, so `new_dex_flow_trigger_target` turns a redelivery into a duplicate Flow start. Stored inbox payloads use the Go connector's JSON field names, so Go and Python replicas can replay each other's pending notifications.

The [`inbound_conversation` example](examples/inbound_conversation) starts one Flow per new conversation and reads it back with `getConversation`.

## Test

```bash
uv sync --locked
uv run ruff check . && uv run mypy --strict src && uv run pytest tests
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/read_conversation/integration_test.py examples/inbound_conversation/integration_test.py
```

`tests/fixtures/conversation-event-go-decoder-cases.json` records the Go decoder's outcome and payload for each notification body in `tests/test_conversation_event_go_parity.py`.

Live provider authorization, behavior, and webhooks remain unverified.
