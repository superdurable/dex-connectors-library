# Mailchimp Python connector

`dex-connector-intuit-mailchimp` v0.1.0 implements the Mailchimp Marketing API
`getMember`, `listMembers`, `upsertMember`, `updateMemberTags`, and `sendCampaign`
operations as typed Dex Steps. The manifest generates their models, definitions,
connection bindings, Step factories, and mocks. The provider client and operation
invocations are hand written. `sendCampaign` checkpoints a dispatch before its
irreversible POST and reads campaign status before any retry.

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

## Verify

```sh
uv run ruff check .
uv run mypy --strict src tests examples
uv run pytest tests
```

From the repository root,
`make test-connector-conformance CONNECTOR_DIRECTORY=connectors/intuit/mailchimp`
replays 65 Go-recorded
provider scenarios against this package. They cover each branch, malformed
addresses and provider bodies, final 429 and
5xx retries, transport failure, complete input fields, campaign uncertainty,
and an earlier send checkpoint. With an isolated Dex development server,
`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:25401 uv run pytest
examples/get_member/integration_test.py` exercises the real Worker and Client
with a local provider stand-in.

No live Mailchimp account was used for this port. Bearer authorization, the
live API responses and error titles, contact and tag idempotency, and campaign
send state transitions remain unverified against Mailchimp itself.
