# Slack Python connector

Install with `pip install dex-connector-slack==0.6.0`.

This package exposes Dex Query Steps for Slack channel pages, the connected workspace identity, channel history pages, thread pages, and replies, plus durable Mutation Steps for posting channel messages, thread replies, and messages with Block Kit buttons. From 0.5.0 it also implements the `channelThreadCreated`, `threadReplyCreated`, and `buttonClicked` Triggers at parity with the Go connector. It uses the static `ConnectionName` configured in Dex Web. OAuth credentials are resolved at call time; the generated project connection refreshes rotating bot and user tokens through the manifest declaration.

For real Slack use, create a Slack app in [Your Apps](https://api.slack.com/apps), configure OAuth, Socket Mode, Event Subscriptions, and, for buttons, Interactivity as described in the [connector guide](../README.md), then connect it in Dex Web.

Every Query Step checks the HTTP status before decoding the body: HTTP 429 and 5xx return Retry and honor `Retry-After`, even when Slack sends no JSON body. From 0.4.1 this includes thread pages and replies.

From 0.6.0, the post Steps resend a post whose outcome is unknown, at parity with Go v1.8.0. A connection loss after dispatch, other 5xx, or an `internal_error` or `fatal_error` without a timestamp returns Retry, and the next attempt resends with the same `client_msg_id`. Each Step call sends at most `max_sends` times, 3 from the generated definition; the next unknown outcome at that cap selects `uncertain`. A Step factory's `max_sends_override` changes the cap from 1 (never resend) to 10. A checkpointed timestamp is read back and never resent. A 409 or an accepted post without a usable identity selects `uncertain` without a resend, because Slack accepted the post and a resend would certainly duplicate it. Definite refusals are never resent: a refusal of a first send selects `providerRejected`, and a refusal of a resend selects `uncertain`, because an earlier send may have posted. Slack's `client_msg_id` deduplication is unverified, so a resend may post a duplicate. See the [connector guide](../README.md#operations).

## Operations

- `new_list_channel_messages_step` reads one newest-first page of a channel's top-level messages with the bot token: `page_size` 1 through 15 (0 reads 15), optional `oldest_timestamp` and `latest_timestamp` Slack timestamps, `is_inclusive`, and the previous page's `next_cursor` as `cursor`. The [channel history example](examples/channel_messages/README.md) runs it.
- `new_post_button_message_step` posts a top-level message or thread reply whose text section is followed by 1 through 25 `MessageButton`s, each with a unique `action_id`, a label, an optional `value`, and `BUTTON_STYLE_PRIMARY`, `BUTTON_STYLE_DANGER`, or no style. `validate_post_button_message_input` returns the VALIDATION failure the operation selects as `defect` before sending. A button value reaches every member who can see the message, so never put a secret in it.

Both replay the Go-recorded provider scenarios in `tests/test_conformance.py`.

## Triggers

Slack sends each Socket Mode event to only one of an app's open connections, so give every Trigger route of a connection to one `MessageTriggerRunner`. `new_project_message_trigger_runner(project, connection_name, ...)` opens the connection and its stored bindings and wraps each route's target in the binding's durable project inbox, which every replica shares; `ProjectChannelThreadCreatedTriggerRoute`, `ProjectThreadReplyCreatedTriggerRoute`, and `ProjectButtonClickedTriggerRoute` name each stored binding and its target. [`examples/thread_approval/receiver.py`](examples/thread_approval/receiver.py) builds one with a Flow-start target and an RPC target.

`run` replays the inboxes, roots first, then replies, then clicks, and then:

- with an `app_token` in the credential, opens Socket Mode through `apps.connections.open`, records each matching envelope in its inboxes before acknowledging it, and delivers acknowledged events in arrival order, so a retrying Dex target does not delay later acknowledgements. Slack's pings extend a 45-second read timeout; a requested `disconnect` reconnects after one second and failures back off from one second up to 30 seconds. Before each reconnect it replays the inboxes again, so a reply cannot overtake its root;
- without one, on a shared connection, serves the Events API callbacks the host relays at `relayed_trigger_path(connection_name)`, which is `/triggers/slack/` and the unpadded base64url connection name, and registers that route with the host at `/internal/slack-triggers/register`, retrying while the host is unavailable. The runner is an ASGI 3 application for that route, and `handle_request` serves other hosts: 200 once every matching inbox recorded the event, 400 without a valid `Dex-Relay-Signature` or for an invalid body, 413 over 1 MiB, 405 for another method, and 503 while the runner does not receive relayed events. A host relays only Events API messages, so a relayed runner with button routes raises ValueError, as it does when the connection is not shared or the host does not relay its events.

`buttonClicked` receives `block_actions` payloads through Socket Mode only. A click is acknowledged before delivery, within Slack's three seconds, so `new_message_trigger_runner` requires every route's target, message routes included, to come from `new_durable_trigger_target`, and the runner of `new_button_clicked_trigger` always raises; the project runner supplies durable inboxes. A click's event ID joins the team, channel, message timestamp, action ID, and action timestamp; its `ButtonClickEvent` carries IDs and the button's value, never Slack's response URL, trigger ID, or verification token.

`new_channel_thread_created_trigger` and `new_thread_reply_created_trigger` each open their own Socket Mode connection and deliver inline; prefer one runner per connection. The configuration validators raise ValueError for a blank channel, blank or repeated member IDs, and a reply matcher without a poster, as Go does.

`SlackTriggerOptions` sets the Trigger logger, a Socket Mode dialer, the reconnect and read timings, and the 256-envelope delivery queue. The logger receives `slack socket mode connected` (INFO), `slack socket mode disconnected; reconnecting` (INFO), `slack socket mode connection failed; reconnecting` (WARN), `trigger event ignored` (DEBUG, with the reason), `trigger event skipped: undecodable` (WARN), `slack relayed trigger registered` (INFO), `slack relayed trigger registration failed; retrying` (WARN), `slack relayed request refused` (DEBUG for a bad signature), and `slack trigger credentials unavailable; retrying` (WARN), plus the SDK's delivery records. Records carry IDs and Slack enum values only, never message text, tokens, or the Socket Mode URL. Durable inbox records use the Go connector's event and payload field names, so a Go, TypeScript, or Python process of the same project can replay them.

## Examples

- [`examples/thread_approval`](examples/thread_approval/README.md): a root message starts a Flow that reads its thread, and an approver's reply completes it, in Socket Mode or relayed mode.
- [`examples/button_approval`](examples/button_approval/README.md): a root message starts a Flow that posts Approve and Reject buttons, and the first decider's click completes it.
- [`examples/channel_messages`](examples/channel_messages/README.md): read one channel history page.
- [`examples/thread_messages`](examples/thread_messages/README.md): post a channel message and read back its thread.

## Verification

```sh
uv sync --locked
uv run ruff check .
uv run mypy --strict src
uv run pytest tests
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/thread_approval/integration_test.py examples/button_approval/integration_test.py examples/channel_messages/integration_test.py examples/thread_messages/integration_test.py
```

The integration tests need a running `dexcli dev` and use local stand-ins for Slack's Socket Mode, the Slack Web API, and a host's relay and credential broker. Live Slack authorization, Socket Mode delivery, Interactivity, and host relay delivery are unverified.
