# Campaign Monitor client discovery Flow

This runnable Flow calls `listClients`, then `listSubscriberLists` for the first returned client, then `listSubscribers` for the first returned list, with the static `campaignmonitor` Dex Web connection. It stores client IDs and names in an Attribute and completes with the typed client result. An account with no clients or lists completes without the later calls. Optional branches are unwired and fail the Flow when selected. The API key does not enter Flow state.

The same Worker registers `CampaignMonitorListCreate`. Start it with a client ID returned by `listClients`, a unique title, and an unsubscribe setting, for example `{"clientId":"client-1","title":"Campaign updates","unsubscribeSetting":"AllClientLists"}`. It calls `createList` and returns the new list ID. Its mock-backed integration test runs through a real Dex Worker and Client.

The Worker also registers `CampaignMonitorSegmentCreate`. Start it with a list ID from `listSubscriberLists`, a title, and optional rule groups, for example `{"listId":"list-1","title":"Recent readers","ruleGroups":[{"rules":[{"ruleType":"EmailAddress","clause":"CONTAINS @example.com"}]}]}`. It calls `createSegment` and returns the new segment ID. Its integration test runs against the mock through a real Dex Worker and Client.

The Worker also registers `CampaignMonitorDraftCampaignCreate`. Start it with a client ID, unique campaign name, subject, sender and reply addresses, hosted HTML URL, and recipient lists or segments. For example, `{"clientId":"client-1","name":"Autumn issue","subject":"New issue","fromName":"Newsletter Team","fromEmail":"sender@example.com","replyTo":"reply@example.com","htmlUrl":"https://example.com/campaign.html","listIds":["list-1"]}` creates a draft without sending it. Its mock-backed integration test runs through a real Dex Worker and Client.

The Worker also registers `CampaignMonitorSubscriberUnsubscribe`. Start it with a list ID and subscribed email, for example `{"listId":"list-1","email":"member@example.com"}`. It unsubscribes that address and returns its email identity; the list's suppression policy applies. Its mock-backed integration test runs through a real Dex Worker and Client.

## Strict FDG 2.0 generation

From `connectors/campaignmonitor/go`, run:

```bash
dexcli visualize ./examples/client-discovery/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/campaignmonitor-client-discovery-fdg
```

The command must report `valid: true` with no diagnostics.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override campaignmonitor=<artifact-directory>`. In Dex Web Connections select `campaignmonitor / campaignmonitor`. Sign in at [Campaign Monitor](https://login.createsend.com/l) and open Profile image > Account settings > API keys ([guide](https://help.campaignmonitor.com/articles/Knowledge/api-keys)), and save the full account key in secret `api_key`. Leave configuration fields blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/campaignmonitor/go`:

```bash
GOWORK=off go run ./examples/client-discovery
```

Start `CampaignMonitorClientDiscovery` with a unique Flow ID and input `{}`. The Worker has no inbound listener. The provider connection name is fixed in `flow/workflow.go`.

With `dexcli dev` serving at `127.0.0.1:8905`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8905 GOWORK=off go test -race -tags=integration ./examples/client-discovery/flow -count=1`. Live provider authorization remains unverified.
