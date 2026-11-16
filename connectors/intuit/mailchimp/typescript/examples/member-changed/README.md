# Mailchimp member-changed example for TypeScript

This example starts one `MailchimpMemberChangeTypeScript` Flow for every
contact added to or changed in one Mailchimp audience, through the
`memberChanged` Trigger's `audience-member-changes` binding:

1. `RecordMemberChange` validates the delivered change and records it.
2. `ReadChangedMember` calls `newGetMemberStep`, so the record holds the
   contact's current state rather than the polled copy.
3. `CompleteMemberChange` records the delivered status beside the contact
   read back and completes.

The Flow ID is `mailchimp-member-change-<Trigger event ID>`, and the event ID
is stable per contact version, so a redelivered change finds its existing
Flow. The first start records the audience's existing contacts as a baseline
and starts no Flow for them. See the
[connector README](../../../README.md#memberchanged-trigger) for the baseline,
lookback, paging, status filter, and rate limits.

## Configure

The `mailchimp-audience` connection takes the Mailchimp API key, as the
[connector README](../../../README.md#mailchimp-setup) describes, and these
values:

- **Poll interval** on the connection is the delay between polls of the
  audience, at least 5 seconds.
- **Watched audience** (`audienceIdEntry`, required) is the audience ID, such
  as `57afe96172`, from **Audience > More options > Audience settings** in
  Mailchimp. Changing it records a new baseline.
- **Contact statuses** (`memberStatusPicker`) keeps only contacts in the
  selected statuses after the change; selecting none starts a Flow for every
  changed contact.

The Worker reads the saved values once at startup, so restart it after saving.

## Run

From `connectors/intuit/mailchimp/typescript`, build the package, make it
resolvable from the example, and generate strict FDG 2.0:

```bash
npm ci && npm run build
npm install --prefix examples/member-changed --no-save --no-package-lock --ignore-scripts .
cd examples/member-changed
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out ../../build/member-changed --open=false
```

Run `dexcli dev -flow-rendering-dir ./build`, configure the connection and the
binding in Dex Web **Connections**, set `DEX_CONNECTOR_CONFIG_FILE` to the
`connections.json` Dex Web wrote, and start the Worker, which also runs the
Trigger:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  npm run example:member-changed
```

Set `LOG_LEVEL=debug` to see every delivery. Trigger records never carry
contact data or credentials.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the cursor commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
as in Go, so a restarted local Worker records a new baseline and changes made
while it was stopped are not delivered.

## Test

```bash
npx vitest run test/member_changed_workflow.test.ts
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration
```

The integration tests run a real Dex Worker and Client against a fake of one
Mailchimp audience, with project storage shared by two Trigger processes. They
check that the baseline starts no Flow, that each contact change starts exactly
one Flow that reads the contact back, that changes made while the Trigger
process is stopped arrive after a restart, that a 400 for the poll query
records a new baseline without delivering the gap, and that a 429 with
`Retry-After` pauses polling before the change arrives once. Live Mailchimp
polling has not been verified.
