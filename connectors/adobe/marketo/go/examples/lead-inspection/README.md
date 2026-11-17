# Marketo lead inspection Flow

This runnable Worker registers `MarketoLeadInspection`, `MarketoFindLeadsByEmail`, `MarketoCreateLead`, `MarketoUpsertLeadByEmail`, `MarketoListActivities`, `MarketoGetCompanyByExternalID`, `MarketoUpsertCompany`, `MarketoGetOpportunityByExternalID`, and `MarketoUpsertOpportunity` through the static `adobe-marketo` connection. Each Flow stores its result in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/adobe/marketo/go`, run:

```bash
dexcli visualize ./examples/lead-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/marketo-lead-inspection-fdg
dexcli visualize ./examples/lead-inspection/flow/find_leads_workflow.go --schema-version 2.0 --json --out /tmp/marketo-find-leads-fdg
dexcli visualize ./examples/lead-inspection/flow/create_lead_workflow.go --schema-version 2.0 --json --out /tmp/marketo-create-lead-fdg
dexcli visualize ./examples/lead-inspection/flow/upsert_lead_by_email_workflow.go --schema-version 2.0 --json --out /tmp/marketo-upsert-lead-fdg
dexcli visualize ./examples/lead-inspection/flow/list_activities_workflow.go --schema-version 2.0 --json --out /tmp/marketo-list-activities-fdg
dexcli visualize ./examples/lead-inspection/flow/get_company_workflow.go --schema-version 2.0 --json --out /tmp/marketo-get-company-fdg
dexcli visualize ./examples/lead-inspection/flow/upsert_company_workflow.go --schema-version 2.0 --json --out /tmp/marketo-upsert-company-fdg
dexcli visualize ./examples/lead-inspection/flow/get_opportunity_workflow.go --schema-version 2.0 --json --out /tmp/marketo-get-opportunity-fdg
dexcli visualize ./examples/lead-inspection/flow/upsert_opportunity_workflow.go --schema-version 2.0 --json --out /tmp/marketo-upsert-opportunity-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override adobe-marketo=<artifact-directory>`. In Dex Web Connections, select `adobe-marketo` and follow the [connection guide](../../../README.md) to configure a Marketo Custom service. Leave `access_token`, `timeout`, and `maxResponseBytes` blank.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/adobe/marketo/go`:

```bash
GOWORK=off go run ./examples/lead-inspection
```

Start `MarketoLeadInspection` with a unique Flow ID and input `{"leadId":318581}`. Replace the sample ID with a lead ID you can access. Alternatively, start `MarketoFindLeadsByEmail` with `{"email":"sample@example.com"}`, replacing the sample address with an email you may search. Pass `nextPageToken` from the prior result to read another page. The checked-in mock-backed test starts `MarketoCreateLead` and `MarketoUpsertLeadByEmail` with `sample@example.com` and `Sample` as the first name; use an email you control for live writes. Both write Flows require the API role's Read-Write Lead permission. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration tests. Real Marketo authorization, retrieval, and writes remain unverified.
