# Tag new Help Scout conversations

This example receives signed Help Scout webhooks and starts one
`HelpScoutTagNewConversationPython` Flow per new conversation:

1. `receiver.py` opens `new_project_conversation_event_endpoint_runner` for the
   `new-conversations` binding of the `conversationEvent` Trigger, and `main.py`
   serves it with uvicorn at `/webhooks/helpscout`, so a delivery is recorded in
   the binding's durable project inbox before Help Scout receives `200`;
2. the binding keeps only conversations of the inbox chosen with the
   `mailboxPicker` unit, and `accept_new_conversation` admits only a
   `convo.created` event for an `active` conversation;
3. `new_dex_flow_trigger_target` starts the Flow with ID
   `helpscout-convo.created-<conversation id>-<body digest>` and the Trigger
   event ID as request ID, so a redelivered webhook starts no second Flow;
4. `RecordNewHelpScoutConversation` stores the identities in the
   `helpscout-new-conversation-tagging` Attribute;
5. `ReadHelpScoutConversation` calls `getConversation` with the five newest
   threads, and `InspectHelpScoutConversation` records the status and thread types;
6. `TagHelpScoutConversation` calls `updateConversation` to add `dex-triaged`,
   and `CompleteHelpScoutTagging` records the tags and completes the Flow.

Only happy-path branches are wired; another branch fails the Flow with the
connector's safe reason.

## Configure

Start `dexcli dev`. In Dex Web open **Connectors > Help Scout** and create the
connection `helpscout-support`: create an app under **Your Profile > My Apps**
at <https://secure.helpscout.net/>, enter its App ID and App Secret, enter a
`webhook_secret` such as the output of `openssl rand -hex 20`, and leave
`access_token` blank.

The binding's **Inbox** unit is optional. Dex CLI v1.47.3 does not yet read the
Python binding's `mailboxPicker` unit (`dexcli visualize` warns
`connector_configuration_ui`), so to keep one inbox, enter
`{"mailboxId": <inbox ID>}` as the `configuration` of the `new-conversations`
entry in the `triggerBindings` list of `connections.json`. The inbox ID is the
number in the browser address bar when the inbox is open at
<https://secure.helpscout.net/>. `{}` tags new conversations of every inbox.

Create the webhook in Help Scout under **Settings > Workspace > Apps >
Webhooks** with the public HTTPS URL of `http://127.0.0.1:8872/webhooks/helpscout`
(for example through a tunnel), the same secret key, and the conversation
created event. Leave Help Scout's `notification` flag off: its body names only a
URL, which the endpoint answers `400`.

## Run

From `connectors/helpscout/python`, with the `DEX_PROJECT_*` or
`DEX_CONNECTOR_CONFIG_FILE` project configuration that Dex Web writes:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run --with uvicorn python -m examples.tag_new_conversations.main
```

Override `WEBHOOK_BIND_ADDRESS` (default `127.0.0.1:8872`) when needed, and set
`LOG_LEVEL=debug` to see every delivery. To test without Help Scout, send one
delivery signed the way Help Scout signs it; the Flow's reads then need a
conversation your connection can read:

```sh
body='{"id":501,"number":12,"threads":1,"type":"email","status":"active","state":"published","subject":"Double charge on order 88213","mailboxId":123,"createdAt":"2026-09-30T11:58:00Z","primaryCustomer":{"id":238604,"type":"customer","first":"Jane","last":"Smith","email":"jane@acme.example.com"},"tags":[{"tag":"billing"}]}'
signature=$(printf '%s' "$body" | openssl dgst -sha1 -hmac "$HELPSCOUT_WEBHOOK_SECRET" -binary | base64)
curl -i http://127.0.0.1:8872/webhooks/helpscout -H 'X-HelpScout-Event: convo.created' \
  -H "X-HelpScout-Signature: $signature" --data-binary "$body"
```

The same `curl` again answers `200` and starts no second Flow. A body changed
after signing answers `400`. A `convo.tags` event answers `200` and starts
nothing. Stop Dex, send a delivery, restart: the inbox replays it and the Flow
starts.

## Flow Definition Graph

Generate a strict Flow Definition Graph 2.0 with Dex CLI 1.47.3 or newer from
`connectors/helpscout/python`; it reports `valid: true` with the
`new-conversations` binding:

```sh
uv run dexcli visualize ./examples/tag_new_conversations/flow.py \
  --schema-version 2.0 --json --out ./build/tag_new_conversations
```

## Test

```sh
uv run pytest tests/test_tag_new_conversations_example.py tests/test_triggers.py
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/tag_new_conversations/integration_test.py
```

The deterministic tests cover the admission rule, the Flow ID and input
mapping, and the binding's visible field. The integration test runs the
receiver and a real Dex Worker and Client against `tests/helpscout_conversations_fake.py`,
a stand-in for Help Scout's conversation, thread, and tag APIs. It checks that a
delivery recorded by a stopped process replays after a restart, that a new
conversation is read and tagged once, that a redelivery starts no second Flow,
that a forged signature answers `400`, and that a `convo.tags` event, another
inbox, and a pending conversation start nothing. Live Help Scout webhooks have
not been verified.
