# Salesforce record-sync example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Salesforce operation. It links a record in another system to Salesforce:
find the record by a human identifier such as an email address, stamp the
other system's ID on the one match, or create the record by that ID when
nothing matches.

1. `RecordSyncRequest` validates and persists the match value, external ID,
   and field values;
2. `FindMatchingRecords` calls `salesforce.NewQueryRecordsStep` with
   `SELECT Id, :externalIdField FROM :sObjectType WHERE :matchField = :matchValue ORDER BY CreatedDate ASC LIMIT 5`,
   binding the configured names as identifiers and the match value as a
   string literal;
3. `ReviewMatchingRecords` creates when nothing matches, links the one match,
   and completes without writing when several records match
   (`ambiguousMatch`) or the one match already carries another external ID
   (`conflictingExternalId`);
4. `LinkExistingRecord` calls `salesforce.NewUpdateRecordStep` with the field
   values and the external ID;
5. `CreateRecordByExternalId` calls `salesforce.NewUpsertRecordByExternalIDStep`,
   so a retried attempt updates the record an earlier attempt created;
6. `PrepareLinkedReadBack` and `PrepareCreatedReadBack` record the outcome;
7. `ReadBackRecord` calls `salesforce.NewGetRecordStep` for the external ID
   field and every written field;
8. `CompleteRecordSync` checks that the read-back carries the external ID and
   completes with the record.

`ReportLinkRejected` and `ReportCreateRejected` complete as `rejected` with
Salesforce's error codes and field names when it refuses the values, such as
`REQUIRED_FIELD_MISSING` on `LastName`. Every other non-happy branch is
optional and fails the Flow. A matched record that disappears before the link
selects `notFound` and fails the Flow.

## Configure

Follow the [Salesforce Connector setup](../../README.md), then configure the
`salesforce / salesforce-crm` connection in Dex Web **Connections**. The
`FindMatchingRecords` Step exposes three units, and every Salesforce Step in
the Flow uses them:

- **Record object** (`sObjectPicker`): the object to match and create, such as
  Contact or Lead. Blank means Contact.
- **Match field** (`fieldNameInput`): the field whose value must equal
  `matchValue`, such as Email. Blank means Email.
- **External ID field** (`fieldNameInput`, required): the object's field
  marked External ID and Unique, such as `ERP_Id__c`. Create it under
  Setup > Object Manager > the object > Fields & Relationships > New before
  running the Flow.

The Worker loads the units once at startup, so restart it after saving them.
The connected user needs read and edit access to the object and both fields,
and create access to the object.

## Run

Generate strict FDG 2.0 from `connectors/salesforce` with the dexcli release
pinned in the repository's `.dex-compat-version` file:

```bash
mkdir -p build
dexcli visualize ./examples/record-sync/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/salesforce-record-sync
```

Run `dexcli dev` with that build directory, then run the Worker with the
connection path shown by Dex Web:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/record-sync
```

The default Worker address is `127.0.0.1:8827`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `SalesforceRecordSync` with a unique Flow ID:

```json
{
  "matchValue": "priya@meridian.example.com",
  "externalId": "ERP-88213",
  "fields": {"LastName": "Raman", "Title": "CTO"}
}
```

`fields` holds text values, which suit text, email, phone, picklist, and date
fields; an application that writes numbers or booleans passes exact JSON
values in the operation input instead. The Flow output reports the status, the
record ID, the candidates of an ambiguous or conflicting match, and the
read-back record. `updatedByExternalId` means the upsert found a record that
already carried the external ID, such as one created by an earlier attempt.

```bash
GOWORK=off go test -race ./examples/record-sync/...
GOWORK=off go test -tags=integration ./examples/record-sync/... -count=1 -v
```

The integration tests run the Flow on a real Dex Server against a stateful
fake Salesforce with the CRM research fixture's decoys: a Lead that shares a
new contact's email, two contacts with one email, a near-duplicate email, and
a contact that already carries another external ID. They also prove that an
upsert whose response is lost is retried into one record, and that a
nine-second upsert that Dex sends twice still leaves one record.
