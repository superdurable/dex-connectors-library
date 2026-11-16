# Mailchimp member-changed example for Python

This example starts one `MailchimpMemberChangePython` Flow for every contact
added to or changed in one Mailchimp audience, through the `memberChanged`
Trigger's `audience-member-changes` binding:

1. `RecordMemberChange` validates the delivered change and records it.
2. `ReadChangedMember` calls `new_get_member_step`, so the record holds the
   contact's current state rather than the polled copy.
3. `CompleteMemberChange` records the delivered status beside the contact read
   back and completes.

The Flow ID is `mailchimp-member-change-<Trigger event ID>`, and the event ID
is stable per contact version, so a redelivered change finds its existing
Flow. The first start records the audience's existing contacts as a baseline
and starts no Flow for them. See the
[connector README](../../../README.md#memberchanged-trigger) for the baseline,
lookback, paging, status filter, and rate limits.

## Configure

The `mailchimp-audience` connection takes the Mailchimp API key, as the
[connector README](../../../README.md) describes, and these values:

- **Poll interval** on the connection is the delay between polls of the
  audience, at least 5 seconds.
- **Watched audience** (`audienceIdEntry`, required) is the audience ID, such
  as `57afe96172`, from **Audience > More options > Audience settings** in
  Mailchimp. Changing it records a new baseline.
- **Contact statuses** (`memberStatusPicker`) keeps only contacts in the
  selected statuses after the change; selecting none starts a Flow for every
  changed contact.

The Flow declares both units on its binding. `dexcli visualize` does not yet
read a Python `ConnectorConfigurationUI` value and reports
`connector_configuration_ui`, so render the Go example, which declares the same
connection, binding, and units, to show them in Dex Web. From
`connectors/intuit/mailchimp`:

```bash
mkdir -p build
dexcli visualize ./go/examples/member-change-intake/flow/workflow.go --schema-version 2.0 --json \
  --out ./build/member-change-intake
dexcli dev -flow-rendering-dir ./build
```

Open Dex Web at `http://127.0.0.1:8802`, configure the connection and the
binding under **Connections**, and set `DEX_CONNECTOR_CONFIG_FILE` to the
`connections.json` that Dex Web wrote. The Worker reads the saved values once
at startup, so restart it after saving. From
`connectors/intuit/mailchimp/python`, generate the Python rendering with
`uv run dexcli visualize ./examples/member_changed/flow.py --schema-version 2.0
--json --out ./build/member_changed` and run the Worker, which also runs the
Trigger:

```bash
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.member_changed.worker
```

Set `DEX_BLOB_CACHE_DIR` or `LOG_LEVEL` when needed. Trigger records, without
contact data or credentials, go to the `dex_connectors.sdk.trigger` logger.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the cursor commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
as in Go, so a restarted local Worker records a new baseline and changes made
while it was stopped are not delivered.

## Test

```bash
uv run pytest tests/test_member_changed_example.py
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/member_changed/integration_test.py
```

The integration tests run a real Dex Worker and Client against a fake of one
Mailchimp audience, with project storage shared by two Trigger processes. They
check that the baseline starts no Flow, that each contact change starts exactly
one Flow that reads the contact back, that changes made while the Trigger
process is stopped arrive after a restart, that a 400 for the poll query
records a new baseline without delivering the gap, and that a 429 with
`Retry-After` pauses polling before the change arrives once. Live Mailchimp
polling has not been verified.
