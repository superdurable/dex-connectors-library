# Tag new Help Scout conversations

This example receives signed Help Scout webhooks and starts one
`HelpScoutTagNewConversation` Flow per new conversation:

1. `app.ts` mounts `newProjectConversationEventEndpointRunner` at
   `/webhooks/helpscout` and runs it at once, so a delivery is recorded in the
   binding's durable project inbox even while Dex is unreachable;
2. the `new-conversations` binding of the `conversationEvent` Trigger keeps only
   conversations of the inbox chosen with the `mailboxPicker` unit, and
   `acceptNewConversation` admits only a `convo.created` event for an `active`
   conversation;
3. `newDexFlowTriggerTarget` starts the Flow with ID
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
`access_token` blank. Then open the Flow's `new-conversations` binding, choose
the inbox in the **Inbox** unit (or leave it empty for every inbox), and save.

Create the webhook in Help Scout under **Settings > Workspace > Apps >
Webhooks** with the public HTTPS URL of `http://127.0.0.1:8872/webhooks/helpscout`
(for example through a tunnel), the same secret key, and the conversation
created event. Leave Help Scout's `notification` flag off: its body names only a
URL, which the endpoint answers `400`.

## Run

From `connectors/helpscout/typescript`, with the `DEX_PROJECT_*` or
`DEX_CONNECTOR_CONFIG_FILE` project configuration that Dex Web writes:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run example:tag
```

Override `WEBHOOK_BIND_ADDRESS` (default `127.0.0.1:8872`) when needed, and set
`LOG_LEVEL=debug` to see every delivery. `GET /readyz` answers `200` once the
binding receives deliveries. To test without Help Scout, send one delivery
signed the way Help Scout signs it; the Flow's reads then need a conversation
your connection can read:

```sh
body='{"id":501,"number":12,"threads":1,"type":"email","status":"active","state":"published","subject":"Double charge on order 88213","mailboxId":123,"createdAt":"2026-09-30T11:58:00Z","primaryCustomer":{"id":238604,"type":"customer","first":"Jane","last":"Smith","email":"jane@acme.example.com"},"tags":[{"tag":"billing"}]}'
signature=$(printf '%s' "$body" | openssl dgst -sha1 -hmac "$HELPSCOUT_WEBHOOK_SECRET" -binary | base64)
curl -i http://127.0.0.1:8872/webhooks/helpscout -H 'X-HelpScout-Event: convo.created' \
  -H "X-HelpScout-Signature: $signature" --data-binary "$body"
```

The same `curl` again answers `200` and starts no second Flow. A body changed
after signing answers `400`. A `convo.tags` event answers `200` and starts
nothing. Stop Dex, send a delivery, restart: the runner logs
`replaying pending trigger events` and the Flow starts.

## Flow Definition Graph

Build the package, make it visible to the analyzer, then lint the Flow with
Dex CLI 1.28.0 or newer; it reports `valid: true` with the `new-conversations`
binding and its `mailboxPicker` unit:

```sh
npm run build
npm install --prefix examples/tag-new-conversations --no-save --no-package-lock --ignore-scripts .
cd examples/tag-new-conversations
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

## Test

`npm test` covers the binding's visible field, the admission rule, the Flow ID
and input mapping, and the Trigger target against an unreachable Dex Server.
`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dev-port> npm run test:integration` runs
the application against a real Dex stack and a local Help Scout stand-in.
Live Help Scout webhooks have not been verified.
