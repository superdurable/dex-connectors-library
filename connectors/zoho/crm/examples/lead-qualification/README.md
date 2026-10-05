# Zoho CRM lead-qualification example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
five Zoho CRM operations to qualify one lead:

1. `RecordLeadQualification` validates the contact's email and names, the
   account name, the Deals stage, and an optional owner, and records them;
2. `ReadZohoDealStages` calls `crm.NewListModuleFieldsStep` for Deals, and
   `CheckZohoDealStage` completes as `unknownStage`, writing nothing, unless
   the stage is an offered option of the `Stage` picklist;
3. `FindZohoAccount` calls `crm.NewFindRecordsStep` for accounts whose
   `Account_Name` equals the company, and `ChooseZohoAccount` uses the one
   match, creates the account when none matches, or completes as
   `ambiguousAccount`, writing nothing, when several match;
4. `UpsertZohoAccount` calls `crm.NewUpsertRecordStep` on Accounts with
   `Account_Name` as the duplicate-check field;
5. `UpsertZohoLeadContact` calls `crm.NewUpsertRecordStep` on Contacts with
   `Email` as the duplicate-check field and `Account_Name` set to the account,
   which links the contact to it;
6. `FindZohoOpenDeal` calls `crm.NewFindRecordsStep` for the contact's most
   recently modified deal whose `Contact_Name` is the contact and whose stage
   is not one of `ClosedDealStages`, and `ChooseZohoOpenDeal` completes as
   `noOpenDeal` when there is none;
7. `AdvanceZohoOpenDeal` calls `crm.NewUpdateRecordStep` to set the deal's
   `Stage`, and its `Owner` when `ownerId` is set;
8. `ReadBackZohoDeal` calls `crm.NewGetRecordStep`, and
   `CompleteLeadQualification` completes as `dealAdvanced` when the deal reads
   back with the stage and owner, or `stageNotApplied` when an automation
   changed them.

`ReportZohoContactRejected` completes as `contactRejected` with Zoho CRM's
error codes and field names when it rejects the contact's values. Every other
optional branch is unwired and fails the Flow, such as a locked deal, a missing
record, an invalid response, or a local defect. Both writes are safe to repeat:
a Dex retry, or a repeated dispatch that reaches Zoho CRM after the slow write
was applied, converges on one account, one contact, and one stage value. Zoho
does not document whether two upserts by `Account_Name` or `Email` that arrive
together can both insert; see
[Duplicate safety](../../README.md#duplicate-safety). The Flow's only Attribute,
`zoho-crm-lead-qualification`, holds the request and progress, which the
summary and display RPCs read, because a Connector branch target receives only
the current operation Result.

`ClosedDealStages` lists Zoho CRM's default closed stages, `Closed Won`,
`Closed Lost`, and `Closed Lost to Competition`. An organization with other
closed stages edits that list; the connector passes stage names through.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/zoho/crm` with the latest stable
dexcli release:

```bash
mkdir -p /tmp/zoho-crm-render
dexcli visualize ./examples/lead-qualification/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/zoho-crm-render/lead-qualification
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required` for each connector Step, because a local module
is not a published release.

## Configure and run

This example is part of the connector module, so Dex needs release metadata
built from this source, passed as an override; without it the connection shows
**Unsupported**. The connector has no Studio bundle. From the repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
mkdir -p /tmp/zoho-crm-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/zoho/crm/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/zoho/crm \
  --version v0.21.0 --tag connectors/zoho/crm/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/zoho-crm-release/connector-release.json \
  --digest-output /tmp/zoho-crm-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir /tmp/zoho-crm-render \
  --connector-release-override zoho-crm=/tmp/zoho-crm-release
```

Follow the [Zoho setup](../../README.md#zoho-setup), then open
**Connections** in Dex Web and select `zoho-crm / zoho-crm-sales`, which all
Zoho CRM Steps of `ZohoCRMLeadQualification` use. Choose the **Data center** of
your Zoho CRM account, enter the client ID and secret, and choose
**Authorize**; Dex Web stores the access token, refresh token, and
`api_domain` that Zoho returns. The example has no Step configuration.

In a second terminal, start the Worker from `connectors/zoho/crm`. It reads
the `DEX_PROJECT_*` project configuration environment described in
[project configuration](../../../../../sdkgo/projectconfig/README.md); Dex Web
or Superverse Studio writes that configuration when you save the connection.

```bash
go run ./examples/lead-qualification
```

The Worker reads the connection at startup and the credentials before every
call. The default Worker address is `127.0.0.1:8858`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. For local verification against a Zoho CRM-compatible fake only,
`ZOHO_CRM_LOCAL_API_BASE_URL` replaces `{api_domain}/crm/v8`; it must be HTTPS
or a loopback HTTP URL, and production Workers leave it unset.

In the Run workspace, choose **Start Flow**, select
`ZohoCRMLeadQualification`, choose the Worker at `127.0.0.1:8858`, enter a
unique Flow ID, and submit. `stage` is a Deals `Stage` value exactly as Zoho
CRM stores it, one of the options `ReadZohoDealStages` reads; a value Zoho does
not offer completes as `unknownStage`. `ownerId` is a Zoho CRM user's numeric
ID, as a record's `Owner` lookup shows it, or blank to keep the deal's owner.

```json
{
  "contactEmail": "jane@acme.example.com",
  "contactFirstName": "Jane",
  "contactLastName": "Smith",
  "accountName": "Acme Corp",
  "stage": "Proposal/Price Quote",
  "ownerId": "4150868000000225013"
}
```

The Flow result and the `zoho-crm-lead-qualification` Attribute hold the
`phase`, the account, contact, and deal IDs, whether the account and contact
were created, the deal's stage and owner as read back, and Zoho CRM's error
codes for a rejected contact.

## Test

```bash
GOWORK=off go test -race ./examples/lead-qualification/...
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against the stateful fake in
[`internal/fakecrm`](../../internal/fakecrm), which, like Zoho CRM, has no
idempotency key, answers COQL with `204` when nothing matches, returns only the
ID of a lookup in COQL, reports times in the organization's offset, matches
upserts by duplicate-check field without regard to case, requires `Last_Name`
for a new contact, and enforces the Deals `Stage` picklist:

- a new lead creates the account and a contact linked to it, and completes as
  `noOpenDeal`;
- a repeat lead matches the contact by an email in another letter case and
  advances the contact's newest open deal and owner, leaving an older open
  deal, a newer closed deal, and another contact's deal untouched;
- an unknown or unused stage completes as `unknownStage`, and two accounts
  with the same name complete as `ambiguousAccount`, neither writing anything;
- a contact upsert applied but answered after nine seconds, past Dex's async
  local phase, is dispatched again, and the second upsert updates the contact,
  so one contact exists;
- a deal update held for nine seconds is dispatched again, and both dispatches
  set the same values;
- a lost update response is retried;
- a rate-limited query waits for `Retry-After`;
- a rejected contact completes as `contactRejected` with
  `INVALID_DATA` on `Email` and no Zoho message text;
- a locked deal fails the Flow through the unwired `recordRejected` branch
  without Zoho message text;
- an account name with an apostrophe fails the Flow before any request;
- a token Zoho CRM rejects at the first upsert is refreshed once at the EU
  Zoho Accounts server through a refreshing credential source, requests go to
  `https://www.zohoapis.eu/crm/v8`, the upsert is sent once more, and the
  stored credentials keep the data center, the unrotated refresh token, and
  the returned `api_domain`.

```bash
GOWORK=off go test -tags=integration ./examples/lead-qualification/... -count=1 -v
```
