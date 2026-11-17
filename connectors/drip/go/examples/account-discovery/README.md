# Drip account discovery Flow

The same Worker also registers `DripSubscriberPage`. Start it with `ListSubscribersInput` containing an account ID returned by `listAccounts`, a one-based `page`, and `perPage` from 1 to 1000. It returns subscriber identity and status without custom fields or tracking data. The mock-backed real Dex test is `TestSubscriberPageUsesDefaultMockWithRealDex` in `flow/`; run it with the integration build tag and `DEX_FLOW_SERVICE_ADDRESS` set to the local Dex Flow service.

The Worker registers `DripCampaignPage` as well. Start it with `{"accountId":"9999999","page":1}` using an account ID from `listAccounts`; it returns campaign IDs, names, and statuses. `TestCampaignPageUsesDefaultMockWithRealDex` runs this Flow against real Dex with a connector mock. Optional rejection branches are unwired.

The Worker registers `DripCampaignSubscribers`. Start it with `{"accountId":"9999999","campaignId":"123456"}` using IDs returned by `listAccounts` and `listCampaigns`. It returns one page of subscriber IDs, emails, and statuses; `status`, `page`, `perPage`, and `direction` are optional. `TestCampaignSubscribersUsesDefaultMockWithRealDex` runs it against real Dex with a connector mock. Optional rejection branches are unwired.

This runnable Flow calls `listAccounts` with the static `drip` Dex Web connection. It stores account IDs and names in an Attribute and completes with the typed result. Optional branches are unwired and fail the Flow when selected. The API key does not enter Flow state.

## Strict FDG 2.0 generation

From `connectors/drip/go`, run:

```bash
dexcli visualize ./examples/account-discovery/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/drip-account-discovery-fdg
dexcli visualize ./examples/account-discovery/flow/subscriber_page.go --schema-version 2.0 --lint app --json --out /tmp/drip-subscriber-page-fdg
dexcli visualize ./examples/account-discovery/flow/campaign_page.go --schema-version 2.0 --lint app --json --out /tmp/drip-campaign-page-fdg
dexcli visualize ./examples/account-discovery/flow/campaign_subscribers.go --schema-version 2.0 --lint app --json --out /tmp/drip-campaign-subscribers-fdg
```

The command must report `valid: true` with no diagnostics.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override drip=<artifact-directory>`. In Dex Web Connections select `drip / drip`. Open [Drip user profile](https://www.getdrip.com/user/edit), and save the personal API Token in secret `api_key`. Leave configuration fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/drip/go`:

```bash
GOWORK=off go run ./examples/account-discovery
```

Start `DripAccountDiscovery` with a unique Flow ID and input `{}`. The Worker has no inbound listener. The provider connection name is fixed in `flow/workflow.go`.

With `dexcli dev` serving at `127.0.0.1:8905`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 GOWORK=off go test -race -tags=integration ./examples/account-discovery/flow -count=1`. Live provider authorization remains unverified.
