# Slack Connector

This module reads and writes Slack channel threads and receives Slack Events
API messages through Socket Mode. It supports public and private channels, not
direct messages.

## Slack app setup

1. Create a Slack app and enable Socket Mode.
2. Add bot scopes `channels:history`, `groups:history`, `channels:read`,
   `groups:read`, `users:read`, and `chat:write`.
3. Add user scopes `channels:history` and `groups:history`. Slack requires a
   user token for `conversations.replies` on public and private channels.
4. Subscribe to the `message.channels` and `message.groups` bot events.
5. Install the app, then create an app-level token with `connections:write`.
6. Invite the bot to every private channel it must observe or post to.

Dex Web stores the OAuth bot token and user token. Enter the `xapp-` app-level
token in the host-owned secret field; it is never sent to the Studio iframe.
Use Slack's standard OAuth flow with the host-owned client secret. Slack's
localhost PKCE installation mode cannot request bot scopes.

## Trigger settings

Dex Web shows searchable channel and member pickers. It stores stable channel
IDs such as `C0123456789` and member IDs such as `U0123456789`; names and
avatars are display-only.

`threadTriggerMatcher.messageContains` is optional. Empty text matches every
human top-level message from an allowed poster. `posterUserIds` is also
optional for the root trigger; an empty list means any human poster.

`threadReplyMatcher.posterUserIds` must contain at least one approver. Text
matching is case-insensitive substring matching. For example, `approve` also
matches `disapprove`; use a more distinctive phrase if that is undesirable.

The application owns the mapping from Slack threads to Flow IDs. Pass a
callback that accepts `ThreadIdentity` to `FlowIDByThread`; it receives the team
ID, channel ID, and root timestamp. The RPC target receives the same identity,
so root and reply events resolve to the same Flow. RPC registration is code,
not Trigger configuration. Pass the same direct bound Flow method to
`dex.DefineRPC` and `NewDexRPCTriggerTarget`. The application owns RPC options,
durable state, locking, and event deduplication.

Both Dex targets also require an application-owned `TriggerEventFilter`. It
runs before Flow ID resolution and is the final admission rule for starting a
Flow or invoking an RPC. Provider matchers reduce Socket Mode traffic, while
the application filter can independently enforce channel, poster, message, or
other domain rules. Returning false consumes the event without calling Dex;
returning an error keeps the delivery retryable.

If a picker cannot load, copy IDs manually:

- Open channel details in Slack and choose **Copy channel ID**.
- Open a member profile, open its menu, and choose **Copy member ID**.

## Local configuration

```json
{
  "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
  "connections": [{
    "connectorId": "slack",
    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/slack",
    "moduleVersion": "v0.1.0",
    "provider": "slack",
    "connectionName": "slack-workspace",
    "configuration": {},
    "credentials": {
      "bot_token": "xoxb-...",
      "user_token": "xoxp-...",
      "app_token": "xapp-..."
    }
  }],
  "triggerBindings": [{
    "connectorId": "slack",
    "connectionName": "slack-workspace",
    "triggerName": "channelThreadCreated",
    "bindingName": "slack-thread-approval-start",
    "configuration": {
      "channelId": "C0123456789",
      "threadTriggerMatcher": {"messageContains": "request approval", "posterUserIds": []}
    }
  }, {
    "connectorId": "slack",
    "connectionName": "slack-workspace",
    "triggerName": "threadReplyCreated",
    "bindingName": "slack-thread-approval-reply",
    "configuration": {
      "channelId": "C0123456789",
      "threadReplyMatcher": {"messageContains": "approve", "posterUserIds": ["U0123456789"]}
    }
  }]
}
```

Load the file with `localconfig.LoadFromEnvironment`, create generated local
Trigger runners, and run them beside the Dex Worker. Socket Mode envelopes are
persisted in a binding-specific local inbox and acknowledged before the target
runs, so Slack is not blocked by Dex latency. The runner retries a failed target
while the process remains alive and replays pending events after restart. Flow
starts and approval RPCs deduplicate stable event IDs, so a crash at either side
of inbox cleanup remains safe.

## Operations

- `ListThreadMessages` returns one page and a next cursor. Page size is 1–15.
- `GetThreadReply` reads one reply by timestamp.
- `PostChannelMessage` creates a top-level channel message.
- `PostThreadReply` replies to an existing thread.

Posting uses Dex Call ID as Slack `client_msg_id`. An ambiguous provider result
uses the `uncertain` branch and must be reconciled or manually recovered rather
than automatically resent.

## Example

[`examples/thread-approval`](examples/thread-approval) combines both Triggers,
`ListThreadMessages`, a typed reply RPC, and `PostThreadReply` in one runnable
Flow.
