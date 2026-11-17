# Mailchimp Python connector

`dex-connector-intuit-mailchimp` v0.2.1 implements the Mailchimp Marketing API
`getMember`, `listMembers`, `upsertMember`, `updateMemberTags`, and `sendCampaign`
operations as typed Dex Steps, and the `memberChanged` poll Trigger. It requires
`dex-connectors-sdk` 0.12.0 or later, the release of the poll Trigger kit. The manifest generates their models, definitions,
connection bindings, Step factories, and mocks. The provider client and operation
invocations are hand written. `sendCampaign` sends under the SDK single-dispatch
fence with `max_sends=call.max_sends` (3 by default), reads campaign status before
every send, resends an unknown outcome only while the campaign is still a draft
and the cap allows, and selects `uncertain` at the cap; see the
[connector README](../README.md#duplicate-safety).

## Use in a Flow

The runnable [contact read example](examples/get_member/README.md) binds the
static `mailchimp-audience` connection through Dex project configuration, reads
an audience contact, and persists its safe result. Its input is a Mailchimp
audience ID and bare email address. The key stays in Dex Web Connections and
is resolved at each provider call; it is never a Flow input or Attribute.
Contact operations reject malformed bare addresses before contacting Mailchimp.
Malformed contact or campaign responses select `invalidResponse`.

From this directory:

```sh
uv sync --locked
DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json uv run python -m examples.get_member.main
DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json uv run python -m examples.get_member.start \
  mailchimp-contact-001 examples/get_member/sample-input.json
```

The connection form has two visible values: `api_key` and the optional
`maxResponseBytes`. The manifest authorization guide links to Mailchimp's API
keys page and explains creation, suffix format, secrecy, rotation, and revoke.
The response limit is bytes; leaving it blank keeps the displayed default.
The contact example has no operation-specific configuration unit.

## memberChanged Trigger

`memberChanged` delivers each contact added to or changed in one audience, at
behavior parity with the Go connector; the
[connector README](../README.md#memberchanged-trigger) describes the baseline,
lookback, paging, event IDs, status filter, rate limits, and cursor resets.
The poll cursor token, the keyed snapshot entries, and the Trigger event IDs
match Go byte for byte, so Go and Python replicas share one cursor document and
one Trigger inbox; `tests/test_member_changed_go_parity.py` replays a recording
of the Go fetcher, including HTML-escaped and non-ASCII merge fields, numbers in
exponent form, reordered tags, paging by time and by offset, and every failure
class.

- `new_project_member_trigger_runner(project, connection, routes)` loads each
  binding's saved `audienceId` and `statuses` and records each change in the
  binding's project Trigger inbox before its cursor commits, as
  [`examples/member_changed/worker.py`](examples/member_changed/worker.py) does.
  A binding without a saved audience raises ValueError.
- `new_member_trigger_runner(connection, routes)` takes
  `MemberChangedTriggerRoute` values with a configuration, a target, and an
  optional cursor store.
- `new_member_changed_trigger(...)` keeps its cursor in memory, like Go's
  generated `NewMemberChangedTrigger`, so it records a new baseline on every
  start.

The connection's `pollInterval` (`Config.poll_interval_seconds`, at least 5
seconds) sets the delay between polls; `Client` raises ValueError below it. The
runner sends one request at a time for every binding of a connection. A 429, or
a 403 without an error document, is `mark_rate_limited` with `Retry-After`; a
400 for the poll query, an undecodable cursor, or a cursor for another audience
is `mark_cursor_invalid`, which records a new baseline.

The [`member_changed` example](examples/member_changed/README.md) starts one
`MailchimpMemberChangePython` Flow per change and reads the contact back with
`new_get_member_step`.

## Verify

```sh
uv run ruff check .
uv run mypy --strict src tests examples
uv run pytest tests
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/member_changed/integration_test.py
```

From the repository root,
`make test-connector-conformance CONNECTOR_DIRECTORY=connectors/intuit/mailchimp`
replays the Go-recorded
provider scenarios against this package. They cover each branch, malformed
addresses and provider bodies, final 429 and
5xx retries, transport failure, complete input fields, a resend below the send
cap, read-back of an earlier send, campaign uncertainty at the cap, and a
pre-fence checkpoint. With an isolated Dex development server,
`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:25401 uv run pytest
examples/get_member/integration_test.py` exercises the real Worker and Client
with a local provider stand-in.

No live Mailchimp account was used for this port. Bearer authorization, the
live API responses and error titles, contact and tag idempotency, and campaign
send state transitions remain unverified against Mailchimp itself, as do the
Trigger's live assumptions: whether `since_last_changed` is exclusive at
one-second resolution, how soon a change becomes listable, and whether
Mailchimp ever sends `Retry-After`.
