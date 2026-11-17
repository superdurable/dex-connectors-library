# Copper person discovery Flow

This runnable example registers `CopperPersonLookup` for `getPerson`, `CopperPeopleSearch` for `listPeople`, `CopperCompanySearch` for `listCompanies`, `CopperLeadSearch` for `listLeads`, and `CopperLeadCreation` for `createLead` with the static `copper` Dex Web connection. Each Flow stores its public result in an Attribute and completes with the typed result. Optional branches are unwired and fail the Flow when selected. Credentials do not enter Flow state.

## Strict FDG 2.0 generation

From `connectors/copper/go`, run:

```bash
dexcli visualize ./examples/person-lookup/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/copper-person-lookup-fdg
dexcli visualize ./examples/person-lookup/flow/list_people.go --schema-version 2.0 --lint app --json --out /tmp/copper-people-search-fdg
dexcli visualize ./examples/person-lookup/flow/list_companies.go --schema-version 2.0 --lint app --json --out /tmp/copper-company-search-fdg
dexcli visualize ./examples/person-lookup/flow/list_leads.go --schema-version 2.0 --lint app --json --out /tmp/copper-lead-search-fdg
dexcli visualize ./examples/person-lookup/flow/create_lead.go --schema-version 2.0 --lint app --json --out /tmp/copper-lead-creation-fdg
```

The command must report `valid: true`. Before the first release, `connector_release_required` warns that automatic Dex Web configuration requires a published release.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override copper=<artifact-directory>`. In Dex Web Connections select `copper / copper`. Open [Copper](https://app.copper.com/) and navigate to **Settings > Integrations > API Keys**. Generate a key, save it in secret `api_key`, and enter the generating user's email in `user_email`. Leave configuration fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/copper/go`:

```bash
GOWORK=off go run ./examples/person-lookup
```

Start `CopperPersonLookup` with a unique Flow ID and input `{"personId":"26443553"}`, replacing the sample with a positive person ID from Copper. The Worker has no inbound listener. The provider connection name is fixed in `flow/workflow.go`.

Start `CopperPeopleSearch` with a unique Flow ID and input `{"pageNumber":1,"pageSize":25,"name":"Sample Person"}`. Omit `name` and `email` to list all people; use the returned page size to decide whether to request the next page.

Start `CopperCompanySearch` with a unique Flow ID and input `{"pageNumber":1,"pageSize":25,"emailDomain":"example.com"}`. Omit filters to list companies; Copper's email domain is a non-secret unique lookup key.

Start `CopperLeadSearch` with a unique Flow ID and input `{"pageNumber":1,"pageSize":25,"email":"lead@example.com"}`. Omit the email to list leads. The email filter is not a unique lookup key: multiple leads can share it.

Start `CopperLeadCreation` with a unique Flow ID and input `{"name":"Sample Lead","email":"lead@example.com"}`. Reconcile existing leads with that name and email first. Copper may accept a create request even if a matching lead already exists.

With `dexcli dev` serving at `127.0.0.1:8905`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 GOWORK=off go test -race -tags=integration ./examples/person-lookup/flow -count=1`. Live provider authorization remains unverified.
