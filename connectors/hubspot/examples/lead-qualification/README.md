# HubSpot lead qualification example

This operation-only example qualifies one inbound lead in HubSpot and uses all
four HubSpot operations:

1. `UpsertHubSpotLeadContact` runs `upsertObject` on contacts with `email` as
   the unique ID property, sets the supplied name and company, and assigns the
   configured owner;
2. `FindHubSpotOpenDeal` runs `searchObjects` for the contact's most recently
   modified open deal (`associations.contact`, `pipeline`, and
   `hs_is_closed = false`) in the configured pipeline;
3. `AdvanceHubSpotOpenDeal` runs `updateObject` to set that deal's `dealstage`
   to the configured stage; and
4. `ReadBackHubSpotDeal` runs `getObject` and completes only when HubSpot
   returns the configured stage.

The Flow completes with `phase` `dealAdvanced`, `noOpenDeal` when the contact
has no open deal in the pipeline, or `rejected` with HubSpot's secret-safe
`VALIDATION` failure when HubSpot rejects the contact. Application Steps store
the progress in the `hubspot-lead-qualification` Attribute, which the summary
and display RPCs read, because a Connector branch target receives only the
current operation Result.

The example wires the happy branch of every operation and `providerRejected`
of the upsert. Every other optional branch, such as `notFound` when a deal is
deleted between the search and the update, fails the Flow. Both writes are
idempotent at HubSpot, so a Dex retry or a repeated dispatch of a slow write
converges on one contact and one stage value.

The Flow sets its type to `HubSpotLeadQualification` and names each
application Step explicitly, because Dex Web **Start Flow** starts the Flow and
Step types written in the Flow Definition Graph. The start Step keeps a
`WaitFor` that returns at once, because Dex Web Start Flow invokes `WaitFor` on
the start Step.

## Step configuration

| Step | Unit | Saved value | Blank |
| --- | --- | --- | --- |
| `UpsertHubSpotLeadContact` | `ownerPicker` as **Lead owner** | `{"ownerId": "77"}` | leaves each contact's owner unchanged |
| `AdvanceHubSpotOpenDeal` | `dealStagePicker` as **Qualified deal stage** (required) | `{"pipelineId": "default", "stageId": "qualifiedtobuy"}` | the Worker refuses to start |

The Worker loads both values once at startup; restart it after saving a
change.

## Release baseline

- HubSpot Connector `v0.21.0`
- dexcli `cli-v1.1.0` or a later stable release

## 1. Prepare a clean local test project

Dex Web configures a connection only for an exact released Connector module.
Create a separate project that consumes the release, then copy the Flow source
so dexcli analyzes it as an application dependency:

```bash
mkdir hubspot-lead-qualification-e2e
cd hubspot-lead-qualification-e2e
mkdir -p flow build

curl -fsSL \
  https://raw.githubusercontent.com/superdurable/dex-connectors-library/refs/tags/connectors/hubspot/v0.21.0/connectors/hubspot/examples/lead-qualification/flow/workflow.go \
  -o flow/workflow.go

go mod init example.com/hubspot-lead-qualification-e2e
go mod edit -go=1.24.0
go get github.com/superdurable/dex-connectors-library/connectors/hubspot@v0.21.0
go mod tidy
```

`go mod edit -go=1.24.0` keeps the project's Go version at or below the Go
release that built dexcli. Otherwise dexcli reports `go_type_check_failed`.

Generate the Flow Definition Graph used by Dex Web:

```bash
dexcli visualize ./flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/hubspot-lead-qualification
```

The command must finish without blocking diagnostics and create
`build/hubspot-lead-qualification.json` with `"valid": true`. The repository's
compatibility gate runs the same analysis twice from a clean consumer module
and requires identical output.

## 2. Start Dex

```bash
dexcli dev \
  --flow-rendering-dir "$PWD/build"
```

Record the Dex Web URL and Dex Server address that dexcli prints. With the
default ports they are `http://127.0.0.1:8802` and `127.0.0.1:8801`.

## 3. Create a HubSpot private app

Sign in to https://app.hubspot.com/ as a super admin, open **Development** >
**Legacy apps** > **Create legacy app** > **Private**, and add the scopes
`crm.objects.contacts.read`, `crm.objects.contacts.write`,
`crm.objects.companies.read`, `crm.objects.companies.write`,
`crm.objects.deals.read`, `crm.objects.deals.write`, and
`crm.objects.owners.read`. Create the app, open **Auth**, choose **Show
token**, and copy the `pat-na1-...` or `pat-eu1-...` token.

The HubSpot OAuth method also exists, but Dex Web `cli-v1.1.0` rejects
HubSpot's OAuth callback because it reads granted scopes only from the RFC 6749
`scope` string; see the [connector README](../../README.md#authentication).

## 4. Configure the connection in Dex Web

Open **Connections** and select `hubspot / hubspot-crm`. It lists the
`upsertObject` and `updateObject` Step uses that have configuration.

1. In **Authorize**, keep **Private app access token**, paste the token into
   `access_token`, leave the optional settings blank, and save the
   credentials. The connection status becomes **Ready**.
2. In **upsertObject**, choose **Load owners**, pick the lead owner or leave
   the owner blank, and choose **Save**.
3. In **updateObject**, choose **Load deal pipelines**, pick the pipeline and
   the qualified stage, and choose **Save**.

When a list cannot load, each picker accepts the ID directly: owner IDs are in
HubSpot **Settings** > **Users & Teams**, and pipeline and stage IDs are in
**Settings** > **Objects** > **Deals** > **Pipelines**.

## 5. Run the Worker

The Worker reads the `DEX_PROJECT_*` project configuration environment
described in [project configuration](../../../../sdkgo/projectconfig/README.md);
Dex Web or Superverse Studio writes that configuration when you save the
connection.

```bash
export DEX_FLOW_SERVICE_ADDRESS="127.0.0.1:8801"

GOWORK=off go run \
  github.com/superdurable/dex-connectors-library/connectors/hubspot/examples/lead-qualification@v0.21.0
```

The Worker listens on `127.0.0.1:8841` by default. Set
`DEX_WORKER_BIND_ADDRESS` to use another address, and `DEX_BLOB_CACHE_DIR` to
move its blob cache. It logs the loaded pipeline and stage IDs at startup. If
Dex is unreachable, the Worker logs `dex server unavailable; retrying` with a
delay that grows to 30 seconds and starts once Dex answers.

## 6. Start the Flow

In Dex Web, open the `HubSpotLeadQualification` Flow, choose **Start Flow**,
select the Worker address, and enter a contact that has an open deal in the
configured pipeline:

```json
{
  "email": "ada@example.com",
  "firstName": "Ada",
  "lastName": "Lovelace",
  "company": "Analytical Engines"
}
```

Open the run and follow it to **Completed**. The display shows the contact ID,
the deal ID and name, the stage before the update, and the stage HubSpot read
back. In HubSpot, the contact shows the configured owner and the deal shows the
new stage. Use a disposable test contact and deal: the Flow really writes both
records.

## Verify from the repository

The example tests run from the Connector directory:

```bash
GOWORK=off go test -race ./examples/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  GOWORK=off go test -tags=integration ./examples/lead-qualification/flow/... -count=1 -v
```

The integration tests use a real Dex Server and a stateful local fake HubSpot
API. They verify an existing lead whose open deal advances while a closed deal
and another pipeline's deal stay untouched, a duplicate start with the same
request ID, a new lead without an open deal, a rate-limited upsert that Dex
retries after `Retry-After`, a rejected upsert that completes with a
secret-safe failure, a Flow failure for the unwired `notFound` branch, an
expired OAuth connection that refreshes before the first call, and slow
writes. For the slow writes, HubSpot answers the
upsert and the update after nine seconds, Dex's asynchronous fallback
dispatches each one again, and the test proves one contact and the configured
stage remain. That test takes about 35 seconds.

Before `v0.21.0` is published, build a local release artifact with
`go run ./cmd/connectorctl release-artifact` from the repository root and pass
its directory to `dexcli dev --connector-release-override hubspot=DIRECTORY`.
The test project must still resolve `connectors/hubspot@v0.21.0` without a
`replace`, as the compatibility gate does with a local module proxy.
