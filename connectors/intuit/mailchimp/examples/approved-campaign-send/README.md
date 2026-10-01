# Mailchimp approved campaign send example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Mailchimp operation. It enrolls a few people in an audience and tags
them, without touching anyone who opted out, then sends an existing draft
campaign only after a person approves it, and at most once:

1. `RecordCampaignLaunch` validates the audience ID, campaign ID, tag,
   `statusIfNew` (`pending` or `subscribed`), and 1 to 25 contacts, and records
   the launch;
2. for each contact, `ReadLaunchContact` calls `mailchimp.NewGetMemberStep`,
   and `PlanLaunchContact` leaves a contact Mailchimp shows as `unsubscribed`,
   `cleaned`, or `archived` untouched and records it as `suppressed`;
3. otherwise `UpsertLaunchContact` calls `mailchimp.NewUpsertMemberStep` with
   only `statusIfNew` and the `FNAME` and `LNAME` merge fields, so an existing
   contact keeps its status; `providerRejected`, such as a contact in a
   compliance state, records the contact as `rejected` and continues;
4. `TagLaunchContact` calls `mailchimp.NewUpdateMemberTagsStep` to add the
   launch tag;
5. `CountSubscribedAudience` calls `mailchimp.NewListMembersStep` for a
   one-contact page of `subscribed` contacts, whose `totalItems` the approver
   sees;
6. `AwaitSendApproval` sets the phase to `awaitingApproval` and waits. Nothing
   is sent until a person runs **Approve campaign send**;
7. `SendLaunchCampaign` calls `mailchimp.NewSendCampaignStep` with sync
   durability and `expectedListId` set to the launch audience. `sent` and
   `alreadySent` complete as `sent` or `alreadySent`; `notSendable`,
   `notFound`, and `providerRejected` complete as `notSent` with the reason;
   `uncertain` parks the Flow as `needsReview`.

Unwired optional branches, such as an invalid response or a local defect, fail
the Flow.

## Actions

Each Action requires the `mailchimp-campaign.send` permission and takes the
name of the person acting and an optional note:

- **Approve campaign send** is available in `awaitingApproval`. It records the
  approval and schedules the one send. Running it again, or a second person
  approving at the same moment, changes nothing, because only the
  `awaitingApproval` phase schedules a send and both RPCs lock the phase.
- **Decline campaign send** is available in `awaitingApproval` and completes
  as `declined` without reading or sending the campaign.
- **Recheck campaign and send if still a draft** is available in
  `needsReview`, after a send whose outcome is unknown. Check the campaign on
  Mailchimp's **Campaigns** page first. The recheck runs `sendCampaign` as a
  new Step execution: its read completes as `alreadySent` while Mailchimp
  shows the campaign sending or sent, and it sends only a campaign that is
  still a draft for this audience.

## Configure

Follow the [Mailchimp setup](../../README.md#mailchimp-setup), then save the
API key for the `mailchimp-audience` connection under **Connections** in Dex
Web; all five Mailchimp Steps of `MailchimpApprovedCampaignSend` use it. The
data center comes from the key, so the connection has no other required field.
The Flow has no Studio configuration units: Studio commands cannot reach the
per-data-center Mailchimp host, so the audience and campaign are Start Flow
input. Find the audience ID under **Audience > More options > Audience
settings > Audience ID**. The campaign ID is the API `id`, letters and digits
such as `42694e9e57`, not the number after `id=` in the Mailchimp web app
address, which is the campaign's `web_id`. List draft campaigns with
`GET https://<dc>.api.mailchimp.com/3.0/campaigns?status=save&fields=campaigns.id,campaigns.web_id,campaigns.settings.title`
and the same API key, or use the `id` Mailchimp returned when the campaign was
created through the API.

## Run

Generate strict FDG 2.0 from `connectors/intuit/mailchimp` with the dexcli
release pinned in the repository's `.dex-compat-version` file:

```bash
mkdir -p build
dexcli visualize ./examples/approved-campaign-send/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/approved-campaign-send
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required` for each connector Step, because a local module
is not a published release.

This example is part of the connector module, so Dex needs release metadata
built from this source, passed as an override; without it the connection
shows **Unsupported**. From the repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
mkdir -p /tmp/mailchimp-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/intuit/mailchimp/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp \
  --version v0.1.0 --tag connectors/intuit/mailchimp/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/mailchimp-release/connector-release.json \
  --digest-output /tmp/mailchimp-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/intuit/mailchimp/build" \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override mailchimp=/tmp/mailchimp-release
```

In a second terminal, start the Worker from `connectors/intuit/mailchimp` with
the connection file Dex Web shows:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/approved-campaign-send
```

The default Worker address is `127.0.0.1:8832`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. For local verification against a Mailchimp-compatible fake only,
`MAILCHIMP_LOCAL_API_BASE_URL` replaces the host derived from the key; it must
be HTTPS or a loopback HTTP URL, and production Workers leave it unset.

Start `MailchimpApprovedCampaignSend` with a unique Flow ID:

```json
{
  "listId": "57afe96172",
  "campaignId": "42694e9e57",
  "tag": "spring-launch",
  "statusIfNew": "pending",
  "contacts": [
    {"emailAddress": "ben@example.com", "firstName": "Ben"},
    {"emailAddress": "dana.diaz@example.com", "firstName": "Dana", "lastName": "Diaz"}
  ]
}
```

Use `statusIfNew: subscribed` only for people who already gave permission;
`pending` makes Mailchimp send each new contact its confirmation email. The
Flow result and the `mailchimp-campaign-launch` Attribute hold the phase,
each contact's action and status, the subscribed count, the approval, and the
campaign as read before the send.

## Test

```bash
GOWORK=off go test -race ./examples/approved-campaign-send/...
```

With the pinned Dex development server running, the integration tests drive
the Flow on a real Worker against a stateful fake Mailchimp that, like
Mailchimp, has no idempotency key:

- an approved launch reads every contact, never writes or tags an unsubscribed
  one, upserts with `status_if_new` and no `status`, addresses a mixed-case
  address by its lowercased hash, sends nothing before approval, and sends
  once after it;
- a declined launch completes without reading or sending the campaign;
- two approvals at the same moment schedule one send;
- an upsert and a tag update each held for nine seconds, past Dex's async local
  phase, are dispatched again, and both attempts leave one contact, one
  confirmation email, and one tag;
- a send held for nine seconds is sent exactly once, because the Step is sync;
- a lost send response parks the Flow as `needsReview` without a resend, and
  the recheck Action completes as `alreadySent` with still one send;
- a Worker lost while Mailchimp holds the send is replaced, and the new
  attempt finds the dispatch checkpoint, reads the campaign as sending, and
  sends nothing;
- a rate-limited send waits for `Retry-After` and sends once;
- an already sent, scheduled, or other-audience campaign is never sent;
- a contact Mailchimp rejects is recorded with only the problem title, and the
  launch continues;
- an invalid launch fails the Flow before any Mailchimp request.

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/approved-campaign-send/... -count=1 -v
```
