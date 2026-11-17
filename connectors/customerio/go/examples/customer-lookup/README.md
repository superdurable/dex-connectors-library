# Customer.io customer lookup Flow

These runnable Flows call `getCustomersByEmail`, `getCampaign`, `listCampaigns`, `getCampaignMetrics`, `upsertCustomer`, and `deleteCustomer` through the static `customerio` connection. They persist typed results in Attributes and expose them through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/customerio/go`, run:

```bash
dexcli visualize ./examples/customer-lookup/flow/workflow.go --schema-version 2.0 --json --out /tmp/customerio-customer-lookup-fdg
dexcli visualize ./examples/customer-lookup/flow/get_campaign.go --schema-version 2.0 --lint app --json --out /tmp/customerio-campaign-fdg
dexcli visualize ./examples/customer-lookup/flow/list_campaigns.go --schema-version 2.0 --lint app --json --out /tmp/customerio-campaigns-fdg
dexcli visualize ./examples/customer-lookup/flow/get_campaign_metrics.go --schema-version 2.0 --lint app --json --out /tmp/customerio-campaign-metrics-fdg
dexcli visualize ./examples/customer-lookup/flow/upsert_customer.go --schema-version 2.0 --lint app --open=false --json --out /tmp/customerio-upsert-customer-fdg
dexcli visualize ./examples/customer-lookup/flow/delete_customer.go --schema-version 2.0 --lint app --open=false --json --out /tmp/customerio-delete-customer-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev -open=false` with `--connector-release-override customerio=<artifact-directory>`. In Dex Web Connections select the `customerio` connection. Follow the [connection guide](../../../README.md) to enter secret `privateKey`, and add `trackingSiteId` and secret `trackingApiKey` for writes; leave `baseUrl`, `trackingBaseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/customerio/go`:

```bash
GOWORK=off go run ./examples/customer-lookup
```

Start `CustomerioCustomerLookup` with a unique Flow ID and input `{"email":"person@example.com"}`. Replace the sample with an email from the Customer.io People page. The Worker connects to Dex Server without an inbound listener.

Start `CustomerioCampaignInspection` with `{"campaignId":5}`, `CustomerioCampaignSearch` with `{}`, and `CustomerioCampaignMetrics` with `{"campaignId":5,"startUnix":1735718400,"endUnix":1735804800,"resolution":"days"}`. Find the non-secret integer campaign ID in the Customer.io Automations page URL or list response. The metrics timestamps are UTC Unix seconds; blank IDs, timestamps, and resolution are invalid. These operations use the same secret App API key stored in Dex Web Connections.

Start `CustomerioCustomerUpsert` with `{"identifier":"person@example.com","attributes":{"first_name":"Ava"}}` or `CustomerioCustomerDeletion` with `{"identifier":"person@example.com"}`. Replace the sample with a real workspace customer ID or email. Customer deletion is permanent; test on a disposable profile. These operations use the Track credentials stored in Dex Web Connections.

CI runs the fixture-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Customer.io authorization and calls remain unverified.
