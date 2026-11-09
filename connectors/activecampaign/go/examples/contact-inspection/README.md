# ActiveCampaign contact-inspection Flow

This runnable Flow calls `listContacts` with the static `activecampaign` connection. Its happy-path contact page is saved in an Attribute and shown through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected. Only basic contact fields enter the Flow result; the API Key does not.

## Strict FDG 2.0 generation

From `connectors/activecampaign/go`, run:

```bash
dexcli visualize ./examples/contact-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/activecampaign-contact-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override activecampaign=<artifact-directory>`. In Dex Web Connections select `activecampaign / activecampaign`. Sign in at https://www.activecampaign.com/login, open Settings > Developer, and copy the account-specific API URL into `baseUrl` and the user's API Key into the secret field. Blank `baseUrl` is invalid; blank timeout and response-size fields use their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/activecampaign/go`:

```bash
GOWORK=off go run ./examples/contact-inspection
```

Start `ActiveCampaignContactInspection` with a unique Flow ID and input `{"offset":0,"limit":20}`. The Worker connects to Dex Server without an inbound listener. The provider connection name is fixed in `flow/workflow.go`.

With `dexcli dev` serving at `127.0.0.1:8905`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 GOWORK=off go test -race -tags=integration ./examples/contact-inspection/flow -count=1`. Live provider authorization remains a separate check.
