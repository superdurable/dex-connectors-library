# Eventbrite organization-inspection Flow

This runnable application registers `EventbriteOrganizationInspection` and `EventbriteEventPublication` with the static `eventbrite` connection. The first Flow calls `listOrganizations`; the second publishes a selected draft event and saves its metadata in an Attribute. Optional branches are unwired and fail the Flow when selected. No Private Token enters a Flow result.

Start `EventbriteEventPublication` with `{"eventId":"67890"}` using a draft event ID from your organization. Eventbrite requires a name, description, organizer, at least one ticket, and valid payment settings before publication. The deterministic integration test uses the generated connector mock; it does not authorize a real Eventbrite account.

## Strict FDG 2.0 generation

From `connectors/eventbrite/go`, run:

```bash
dexcli visualize ./examples/organization-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/eventbrite-organization-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override eventbrite=<artifact-directory>`. In Dex Web Connections select `eventbrite / eventbrite`, open Account Settings > Developer Links > API Keys at https://www.eventbrite.com/account-settings/apps, copy the Private Token, and save it in the secret field. Leave `baseUrl` blank for the displayed API v3 endpoint.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/eventbrite/go`:

```bash
GOWORK=off go run ./examples/organization-inspection
```

Start `EventbriteOrganizationInspection` with a unique Flow ID and input `{"page":1}`. The Worker connects to Dex Server without an inbound listener. The provider connection name is fixed in `flow/workflow.go`.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test -race ./...`.
