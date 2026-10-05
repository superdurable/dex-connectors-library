# Zoho CRM changed-deals example

This example runs one operation-only Flow from Dex Web **Start Flow** that
reads Zoho CRM's change feed for Deals with `listModifiedRecords`, the polling
read an application uses until Zoho CRM notifications can be a Trigger:

1. `RecordChangedDealsRequest` validates where the feed starts, an RFC 3339
   `modifiedSince` instant or the `cursor` an earlier run completed with, and
   the page budget;
2. `ListChangedZohoDeals` calls `crm.NewListModifiedRecordsStep` for one page
   of up to 50 deals with their `Deal_Name`, `Stage`, and `Modified_Time`,
   oldest change first;
3. `CollectChangedZohoDeals` keeps the page's deals and its cursor, reads the
   next page while Zoho reports more changes and the budget allows, and
   otherwise completes.

The Flow completes with the changed deals, the pages read, `hasMore` when the
budget ended before the feed did, and the `cursor`. Start the next run with
that cursor to read the changes since; a run that finds none completes with
the same cursor. Every optional branch is unwired and fails the Flow, such as
a rejected query, an invalid page, or a local defect. The Flow's only
Attribute, `zoho-crm-changed-deals`, holds the progress, which the summary and
display RPCs read.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/zoho/crm` with the latest stable
dexcli release:

```bash
mkdir -p /tmp/zoho-crm-render
dexcli visualize ./examples/changed-deals/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/zoho-crm-render/changed-deals
```

The command must report `valid: true`, with one `connector_release_required`
warning for the connector Step inside this repository.

## Configure and run

Build the release metadata and start `dexcli dev` with the override as the
[lead-qualification example](../lead-qualification/README.md#configure-and-run)
shows, then configure `zoho-crm / zoho-crm-sales` in Dex Web **Connections**.
The example has no Step configuration. Start the Worker from
`connectors/zoho/crm`:

```bash
go run ./examples/changed-deals
```

The default Worker address is `127.0.0.1:8859`; `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, `DEX_BLOB_CACHE_DIR`, and, for local verification
only, `ZOHO_CRM_LOCAL_API_BASE_URL` work as in the lead-qualification example.

In the Run workspace, choose **Start Flow**, select `ZohoCRMChangedDeals`,
choose the Worker at `127.0.0.1:8859`, enter a unique Flow ID, and submit
either a starting instant or a cursor, and optionally `maxPages` from 1 to 20,
5 when blank:

```json
{"modifiedSince": "2026-01-28T13:00:00Z", "maxPages": 5}
```

```json
{"cursor": "2026-01-28T13:00:05Z/4150868000003194012"}
```

## Test

```bash
GOWORK=off go test -race ./examples/changed-deals/...
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against the stateful fake in
[`internal/fakecrm`](../../internal/fakecrm):

- 60 changed deals, ten of which share one second across the page boundary,
  are read in two pages, each deal once, oldest first, and a deal changed
  before the starting instant is left out;
- a deal of the first page that another user changes while the feed is read
  appears again after its change, and no unchanged deal is skipped;
- a run with a one-page budget completes with `hasMore`, the next run
  continues from its cursor to the end, and an idle poll keeps the cursor;
- a foreign cursor, such as a Zoho `page_token`, fails the Flow before any
  request.

```bash
GOWORK=off go test -tags=integration ./examples/changed-deals/... -count=1 -v
```
