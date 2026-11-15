# Mailchimp member-change-intake example

This example starts one `MailchimpMemberChange` Flow for every contact added to
or changed in the watched audience, through the `memberChanged` Trigger:

1. `RecordMemberChange` validates the delivered change and records it.
2. `ReadChangedMember` calls `mailchimp.NewGetMemberStep`, so the record holds
   the contact's current state rather than the polled copy.
3. `CompleteMemberChange` records the contact read back and completes the Flow.

The Flow ID is `mailchimp-member-change-<Trigger event ID>`, and the Trigger
event ID is stable per contact version. A redelivered change therefore finds
its existing Flow instead of starting a second one. Only the happy-path `found`
branch is wired. A contact deleted before the read, or a rejected or invalid
read, fails the Flow.

The first start records the audience's existing contacts as a baseline and
starts no Flow for them. See the
[connector README](../../../README.md#memberchanged-trigger) for passes,
pagination, rate limits, and cursor resets.

## Configure

Follow the [Mailchimp Connector setup](../../../README.md#mailchimp-setup),
then configure the `mailchimp-audience` connection under **Connections** in Dex
Web.

- **Poll interval** on the connection is the delay between polls, at least 5
  seconds. Shorter intervals deliver changes sooner and use more of the 10
  simultaneous requests Mailchimp allows each user.
- **Watched audience** on the `audience-member-changes` Trigger binding is the
  audience ID, such as `57afe96172`. In Mailchimp choose **Audience**, pick the
  audience in the **Audience** drop-down if there are several, then
  **More options > Audience settings** and copy **Audience ID**. It is required.
- **Contact statuses** on the same binding keeps only contacts in the selected
  statuses after their change, such as `subscribed` for new sign-ups. Select
  none to start a Flow for every changed contact.

The Worker reads the saved values once at startup, so restart it after saving.
A changed audience records a new baseline.

## Run

Generate strict FDG 2.0 from `connectors/intuit/mailchimp/go` with the latest
stable dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/member-change-intake/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/member-change-intake
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/member-change-intake
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the cursor commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline. Changes made while it was
stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/member-change-intake/...
GOWORK=off go test -tags=integration ./examples/member-change-intake/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for Mailchimp's contact list. They check that the baseline starts no
Flow, that each change starts exactly one Flow, and that changes made while the
Trigger process is stopped arrive after a restart. They also check that a 400
for the poll query records a new baseline without delivering the gap, and that
a 429 with `Retry-After` pauses polling and then delivers the change once. Live
Mailchimp polling has not been verified.
