# Help Scout Python connector

`dex-connector-helpscout` provides Dex-native Queries for Help Scout Inbox conversation search, conversation detail, and customer lookup, plus Mutations for replies, notes, and conversation updates. From `0.2.0` it also implements the signed `conversationEvent` webhook Trigger at parity with Go.

## Install

```sh
uv add dex-connector-helpscout==0.2.1
```

This package requires `dex-connectors-sdk>=0.11.0,<1` and `dex-python-sdk>=1.28.0`.

## Configure a connection

In Dex Web **Connections**, create a `helpscout` connection named `helpscout`. Sign in at [Help Scout](https://secure.helpscout.net/), open your avatar, then **Your Profile > My Apps**, and create an app. Copy its App ID into `app_id` and its secret App Secret into `app_secret`. Leave `access_token` blank: the generated client credentials driver requests and renews it using Help Scout's token endpoint. Help Scout apps have no scopes. The Free plan has no API access. The app acts with its owner's inbox permissions.

The optional `webhook_secret` is needed only by the `conversationEvent` Trigger: a random string of at most 40 characters that you choose, such as the output of `openssl rand -hex 20`, and also enter as the webhook's Secret Key under **Settings > Workspace > Apps > Webhooks** in Help Scout. Blank is fine for operations only; the webhook endpoint then answers `503` so Help Scout retries. Leave `maxResponseBytes` and `webhookMaxBodyBytes` blank for manifest defaults. `maxResponseBytes` limits successful API response bodies in bytes; increase it only for large conversation and thread pages. `webhookMaxBodyBytes` limits webhook request bodies in bytes; a larger body is answered `413` before its signature is checked.

`new_project_connection(project, "helpscout")` loads the manifest settings and generated private or shared credential decoders. It resolves credentials at call time, uses the generated refresh driver, and retries once after a provider 401 if the host can refresh. The connection and secrets stay outside Flow input, results, and Attributes. Close the connection after Worker shutdown.

## Run the example Flow

[`examples/conversation_search/worker.py`](examples/conversation_search/worker.py) is the Worker entrypoint. It loads the named Dex Web connection and registers `HelpScoutConversationSearch`. From this package directory:

```sh
uv run python -m examples.conversation_search.worker
```

Start the Flow with [`input.example.json`](examples/conversation_search/input.example.json). It returns one page and stores it in Flow persistence. The example wires only `searched`, so optional failure branches fail the Flow. Its static Step annotations include the group and explanation for Flow Definition Graph generation.

## Receive conversation webhooks

`new_project_conversation_event_endpoint_runner(project, connection_name, routes)` opens the project connection and each route's stored `conversationEvent` binding, and returns a `ConversationEventEndpointRunner`. It is an ASGI application to mount at the public HTTPS URL of the Help Scout webhook; its `lifespan` startup replays each binding's durable project inbox before receiving new deliveries, and `handle_request` serves hosts that are not ASGI servers. Every binding of one connection shares one endpoint, which:

- answers `503` until a binding runs, and while the connection has no `webhook_secret`, so Help Scout retries;
- verifies `X-HelpScout-Signature`, the base64 HMAC-SHA1 of the raw body keyed with `webhook_secret`, in constant time, and answers `400` for a missing, malformed, or mismatched signature;
- decodes the ten conversation events from `conversation_webhook_events()`, such as `convo.created` and `convo.customer.reply.created`, acknowledges other valid `X-HelpScout-Event` names with `200` and no record, and answers `400` for a malformed event name or a body that is not a conversation, such as Help Scout's URL-only notification body;
- records each event in the binding's durable inbox before answering `200`.

The event ID is the event, the conversation ID, and the first 32 hex digits of the body's SHA-256, such as `convo.created:501:0f1e...`, because Help Scout sends no delivery ID: a redelivered body keeps its ID, and a later event about the same conversation has another. `occurred_at` is the receipt time, because Help Scout sends no event time. The payload is a `ConversationEvent` with the `event` name and the `conversation` as the body described it; its threads are not included, so a Flow reads them with `getConversation`. A binding's stored configuration filters by `events` (empty accepts every conversation event) and `mailboxId` (zero accepts every inbox), as the `mailboxPicker` unit stores it. Webhook verification renews the stored access token only when its record has expired.

Inbox payloads use the Go connector's JSON field names, so Go, TypeScript, and Python replicas of one binding replay each other's events. `new_conversation_event_trigger` builds one binding's runner on `conversation_event_webhook_endpoint(connection)` without a durable inbox, for tests and custom hosts. The runner's `logger` receives the endpoint's, bindings', and inboxes' delivery records; for a custom host, pass `logger=` to `Client`. Records carry event IDs, never bodies or secrets.

## Run the tagging example

[`examples/tag_new_conversations`](examples/tag_new_conversations/README.md) starts one `HelpScoutTagNewConversationPython` Flow per new active conversation, reads it with `getConversation`, and tags it `dex-triaged` with `updateConversation`.

## Operation contract

`searchConversations` combines inbox, status, tag, customer email, modification time, and page filters. `getConversation` reads one conversation and the newest thread page, and selects `merged` for Help Scout's redirect to another conversation. `findCustomerByEmail` returns all matches on its first page, up to 50; no match selects `notFound`.

`replyToConversation` uses Dex's durable single-dispatch checkpoint. It resends a reply or internal note after an ambiguous outcome while the manifest send cap allows, and selects `uncertain` at the cap when the provider may have accepted it without a confirmable thread ID. A confirmed ID is read back on retry. `updateConversation` compares the current state, sends absolute status, assignee, and tag writes only when needed, and reads the final state back. Repeat attempts converge on the requested values.

Every operation uses the generated Step factory and its stable branch IDs. Only the required happy-path branch should be wired by default. The provider can return new conversation status strings, which results preserve unchanged. Treat provider text and HTML as untrusted before rendering.

## Verify locally

```sh
uv sync
uv run pytest tests -q
uv run ruff check .
uv run mypy src/dex_connectors/helpscout
```

The package suite replays the Go-recorded provider requests and outcomes and the generated OAuth refresh agreement. For the real Dex Worker and Client test, start `dexcli dev` on unused ports, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, then run `uv run pytest integration/conversation_search.py examples/tag_new_conversations/integration_test.py -q`. `tests/test_triggers.py` replays deliveries recorded from the Go endpoint in `tests/fixtures/go-conversation-webhook-cases.json` and compares each status, event ID, receipt time, and stored payload. The provider stand-ins use an OS-selected transport address; no live Help Scout authorization was available during this port, so real provider token minting, Inbox calls, and live webhook deliveries remain unverified.
