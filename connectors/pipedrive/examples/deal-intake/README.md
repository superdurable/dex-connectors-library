# Pipedrive deal intake

A runnable Dex Web **Start Flow** example that uses every Pipedrive operation
in one Flow, `PipedriveDealIntake`:

1. `RecordPipedriveLead` validates the lead: a plain `email`, `name`,
   `dealTitle`, and optional `organization` and `source`.
2. `FindPipedriveOrganization` (`searchObjects`) runs an exact organization
   name search; `RoutePipedriveOrganization` links the person only when exactly
   one organization has that name, so a look-alike such as `Acme Corp (EU)` is
   never linked. The Flow never creates organizations.
3. `UpsertPipedriveLeadPerson` (`upsertObject`, sync) creates or updates the
   person by email with the name, organization, and owner. Several persons with
   the email, a rejection, or an unconfirmed earlier create complete the Flow
   in the `needsReview` phase without writing.
4. `FindPipedriveOpenDeal` (`listObjects`) lists the person's most recently
   changed open deal in the configured pipeline.
5. `AdvancePipedriveDeal` (`updateObject`) moves that deal to the configured
   stage and owner, or `CreatePipedriveDeal` (`createObject`, sync) creates it
   there with the lead source in the picked deal custom field. A rejected or
   unconfirmed create completes in `needsReview`; a later attempt does not
   send the create again, except in the narrow case of a Worker lost before Dex
   stored its dispatch checkpoint (see the connector README's duplicate safety
   section).
6. `ReadBackPipedriveDeal` (`getObject`) confirms the stored stage and
   `CompletePipedriveDealIntake` completes with the `DealIntake` output.

## Release baseline

- Pipedrive Connector `v0.21.0`
- dexcli `cli-v1.5.0` or a later stable release

## Generate the Flow Definition Graph

Dex Web configures a connection only for an exact released Connector module.
Create a separate project that consumes the release, then copy the Flow source
so dexcli analyzes it as an application dependency:

```bash
mkdir pipedrive-deal-intake-e2e
cd pipedrive-deal-intake-e2e
mkdir -p flow build

curl -fsSL \
  https://raw.githubusercontent.com/superdurable/dex-connectors-library/refs/tags/connectors/pipedrive/v0.21.0/connectors/pipedrive/examples/deal-intake/flow/workflow.go \
  -o flow/workflow.go

go mod init example.com/pipedrive-deal-intake-e2e
go mod edit -go=1.24.0
go get github.com/superdurable/dex-connectors-library/connectors/pipedrive@v0.21.0
go mod tidy

dexcli visualize ./flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/pipedrive-deal-intake
```

`go mod edit -go=1.24.0` keeps the project's Go version at or below the Go
release that built dexcli; otherwise dexcli reports `go_type_check_failed`. The
command must finish without blocking diagnostics and create
`build/pipedrive-deal-intake.json` with `"valid": true`. The repository's
compatibility gate runs the same analysis twice from a clean consumer module
and requires identical output.

## Configure and run

1. Start Dex with `dexcli dev`, open Dex Web **Connections**, and add the
   Pipedrive connection named `pipedrive-crm`. A Personal API token connection
   lets the pickers list users, pipelines, stages, and custom fields; with the
   OAuth method, type the numeric IDs and the 40-character field key.
2. Configure `UpsertPipedriveLeadPerson`: **Lead owner** (`ownerPicker`,
   optional) stores the user ID written as `owner_id` on the person and on the
   deal; blank sets no owner.
3. Configure `CreatePipedriveDeal`: **Deal stage** (`dealStagePicker`,
   required) stores the pipeline and stage; **Lead source field**
   (`customFieldPicker`, optional) stores a deals text custom field that
   receives the Start Flow `source`.
4. Run the Worker with the `DEX_PROJECT_*` environment that Dex Web shows, as
   described in [project configuration](../../../../sdkgo/projectconfig/README.md):

   ```bash
   GOWORK=off go run .
   ```

   `DEX_WORKER_BIND_ADDRESS` defaults to `127.0.0.1:8847`, and
   `DEX_FLOW_SERVICE_ADDRESS` to Dex's local default. The Worker refuses to
   start until the deal stage is saved.
5. Choose **Start Flow** for `PipedriveDealIntake` with input such as
   `{"email":"jane@acme.example.com","name":"Jane Smith","organization":"Acme Corp","dealTitle":"Acme platform deal","source":"webinar"}`.

## Tests

```bash
GOWORK=off go test ./...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./flow/... -count=1 -v
```

The integration suite runs 13 scenarios on a real Dex Server against a stateful
fake Pipedrive: a new lead, a repeat lead with decoy deals, a person create and
a deal create that each take nine seconds and are sent once under sync
durability, a nine-second update that async Dex sends twice with the same
values, a lost deal-create response (`uncertain`, never resent), a lost
person-create response that the retry reads back by email, a Worker lost
during a deal create, a burst-limited create, persons that share an email, a
rejected deal, an expired OAuth token refreshed to the company `api_domain`,
and invalid input that fails before any Pipedrive call.
