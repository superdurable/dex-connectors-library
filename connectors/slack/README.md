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

The application owns the mapping from Slack events to Flow IDs. Pass the same
pure `FlowIDResolver` to both targets so root and reply events resolve to the
same Flow. Prefer a readable ID built from team ID, channel ID, and root
timestamp. RPC registration is code, not Trigger configuration. Pass the same
direct bound Flow method to `dex.DefineRPC` and `NewDexRPCTriggerTarget`. Use an
`RPCInputMapper` to convert the provider event into an application-owned RPC
input. The application owns RPC options, durable state, locking, and event
deduplication.

Both Dex targets also require an application-owned `TriggerFilter`. It
runs before Flow ID resolution and is the final admission rule for starting a
Flow or invoking an RPC. Provider matchers reduce Socket Mode traffic, while
the application filter can independently enforce channel, poster, message, or
other domain rules. Returning false consumes the event without resolving a
Flow ID, mapping input, or calling Dex. Filters, resolvers, and mappers are
deterministic, side-effect-free functions without error results.

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
    "moduleVersion": "v0.10.0",
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

Load the file with `localconfig.LoadFromEnvironment`, then create one
`NewLocalMessageTriggerRunner` containing every Slack message Trigger route for
the connection. Slack distributes Socket Mode events among active WebSocket
connections, so separate root and reply runners must not compete for the same
workspace events. The shared runner keeps a separate configuration, target,
and durable inbox for each binding while receiving every message through one
socket.

Socket Mode envelopes are persisted in every matching binding-specific inbox
and acknowledged before their targets run. Delivery then stays inline on the
socket reader, so events reach Dex in the order the runner receives them. A
root message is therefore delivered before any reply received after it. If a
connection drops, the runner delivers the events it had persisted but not yet
delivered before it reads envelopes from the next connection. Slack does not
guarantee event order, so a reply that Slack sends before its root is consumed
as undeliverable. The runner hands each event to `sdkgo.DeliverTrigger`:

- An undeliverable event is consumed. Examples are a reply in a thread whose
  Flow finished, failed, or never started. One stray reply therefore cannot
  block the other bindings.
- Any other failure, such as a Dex or Worker outage, retries with backoff from
  250 milliseconds up to 30 seconds. While an event retries, the runner does not
  read later envelopes, so Slack may redeliver them.

At startup and before every reconnect, the runner replays pending events in
order, roots first, with the same backoff, and only then opens the socket.
Replay never ends the process because of one event. Flow starts deduplicate by
event ID, and an approval RPC must also treat a repeated event ID as a
duplicate, so a crash at either side of inbox cleanup remains safe.

Slack's servers ping every Socket Mode connection. The runner answers each ping
and treats it as proof that the connection is alive, so a quiet channel keeps
one connection open. A connection that receives neither an envelope nor a ping
for 45 seconds is closed and replaced. When Slack sends a `disconnect`
envelope, such as `refresh_requested` every few hours, the runner reconnects
after one second. A failed connection attempt is retried after one second,
doubling up to 30 seconds while attempts keep failing. The delay starts again
at one second once a connection receives Slack's `hello`. Canceling the
runner's context closes the socket at once.

## Logging

The runner logs through `log/slog`, to `slog.Default()` unless you pass
`slack.WithLogger(logger)` to `NewLocalMessageTriggerRunner` or `New`. The
logger also reaches the durable inboxes that `NewLocalMessageTriggerRunner`
creates.

| Level | Message | Attributes |
| --- | --- | --- |
| INFO | `slack socket mode connected` | `reconnect` |
| INFO | `slack socket mode disconnected; reconnecting` | `reason`, `delay` |
| WARN | `slack socket mode connection failed; reconnecting` | `attempt`, `delay`, `error` |
| DEBUG | `trigger event ignored` | `event_id`, `channel`, `reason`, and `subtype` for a message subtype |
| DEBUG | `slack socket mode envelope ignored` | `type`, `envelope_id` |
| WARN | `trigger event skipped: undecodable` | `envelope_id`, `error` |

Every record carries `connector` and `connection`; route records also carry
`trigger` and `binding`. A healthy connection logs one INFO
`slack socket mode connected` record and nothing else until it ends. Slack's
routine refreshes log INFO `slack socket mode disconnected; reconnecting` with
Slack's `reason`, such as `refresh_requested`, `warning`, or `link_disabled`.
Only a failure logs WARN: a missing credential, a rejected
`apps.connections.open` call, a dial error, or a socket that dropped or went
silent. `attempt` counts the connection attempts that failed in a row, and
`delay` is the wait before the next one. When Slack rejects the connection, the
`error` names Slack's error code and the HTTP status, for example
`Slack rejected the Socket Mode connection: invalid_auth (HTTP 200)`; a value
that is not a Slack error code is reported as `unknown`. The `reason` of an
ignored message is one of `missing_event_id`, `not_a_message`, `subtype`,
`bot`, `missing_user`, `channel_mismatch`, `not_a_reply`, `not_a_root`, or
`matcher_mismatch`. Each route logs its own reason, so a root message also
appears as `not_a_reply` for the reply route. Ignored messages are DEBUG
because most channel traffic is ignored.

The runner also emits the `sdkgo` delivery records described in the SDK
README, such as `trigger event skipped: undeliverable` and `trigger delivery
failed; retrying` with each backoff delay. For a live event, those records,
including the durable inbox's and the Dex targets', carry the message's
`channel` and `thread_ts`, so one `thread_ts` finds every record about a
thread. Events that the inbox replays after a restart or reconnect carry only
their `event_id`. Records never contain message text, tokens, or the Socket
Mode URL, whose connection ticket is removed from dial errors.

The generated per-Trigger factories, such as
`NewLocalThreadReplyCreatedTrigger`, pass `WithLogger` only to the Socket Mode
source, and their source records carry no `binding`. Their durable inbox and
runner records go to `slog.Default()`, so call `slog.SetDefault` when you use
them, or use `NewLocalMessageTriggerRunner`.

## Operations

- `ListThreadMessages` returns one page and a next cursor. Page size is 1–15.
- `GetThreadReply` reads one reply by timestamp.
- `PostChannelMessage` creates a top-level channel message.
- `PostThreadReply` replies to an existing thread.

Posting uses Dex Call ID as Slack `client_msg_id`. An ambiguous provider result
uses the `uncertain` branch and must be reconciled or manually recovered rather
than automatically resent.

Every operation uses `defect` for invalid local input, connection configuration,
or Connector contract violations. A conclusive Slack API refusal uses
`providerRejected`; query responses that are malformed or exceed configured
limits use `invalidResponse`. Safe query transport, rate-limit, and availability
failures retry instead of producing a branch.

## Example

[`examples/thread-approval`](examples/thread-approval) combines both Triggers,
`ListThreadMessages`, a typed reply RPC, and `PostThreadReply` in one runnable
Flow.
