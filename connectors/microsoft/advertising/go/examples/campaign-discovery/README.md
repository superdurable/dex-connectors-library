# Microsoft Advertising campaign discovery

This Go example registers five Flows. `MicrosoftAdvertisingCampaignDiscovery` lists an account's campaigns; `CampaignInspectionFlow` reads one campaign ID; `AdGroupDiscoveryFlow` lists the ad groups in one campaign; `KeywordDiscoveryFlow` lists one ad group's keywords; `AdDiscoveryFlow` lists one ad group's responsive search ads. All use the static Dex Web connection name `microsoft-advertising` and store their results in Dex Attributes.

Start `AdGroupDiscoveryFlow` with `{"accountId":"12345","campaignId":"98765"}` after copying `aid` and the campaign ID from **Microsoft Advertising > Campaigns > selected campaign > Ad groups**. Its mock-backed Dex test verifies the typed ad group page. The provider authorization and live ad group calls remain unverified.

Start `KeywordDiscoveryFlow` with `{"accountId":"12345","adGroupId":"33"}` after selecting an ad group under **Campaigns > Ad groups > Keywords**. Its mock-backed Dex test verifies the typed keyword page. Live keyword calls remain unverified.

Start `AdDiscoveryFlow` with `{"accountId":"12345","adGroupId":"33"}` after selecting an ad group under **Campaigns > Ad groups > Ads**. Its mock-backed Dex test verifies the typed ad page. Live ad calls remain unverified.

From the repository root, generate and lint the FDG 2.0 graph:

```sh
dexcli visualize connectors/microsoft/advertising/go/examples/campaign-discovery/flow/workflow.go --schema-version 2.0 --json --out /tmp/microsoft-advertising-fdg --lint app
dexcli visualize connectors/microsoft/advertising/go/examples/campaign-discovery/flow/campaign_inspection.go --schema-version 2.0 --json --out /tmp/microsoft-advertising-inspection-fdg --lint app
dexcli visualize connectors/microsoft/advertising/go/examples/campaign-discovery/flow/keyword_discovery.go --schema-version 2.0 --json --out /tmp/microsoft-advertising-keywords-fdg --lint app
dexcli visualize connectors/microsoft/advertising/go/examples/campaign-discovery/flow/ad_discovery.go --schema-version 2.0 --json --out /tmp/microsoft-advertising-ads-fdg --lint app
```

Create a connection in Dex Web as described in [the connector README](../../../README.md), then run `go run ./examples/campaign-discovery` from `connectors/microsoft/advertising/go`. Start `MicrosoftAdvertisingCampaignDiscovery` with input `{"accountId":"12345"}`, where the ID is the `aid` value in the Microsoft Advertising Campaigns URL. Start `CampaignInspectionFlow` with `{"accountId":"12345","campaignId":"98765"}` to inspect one campaign. The mock integration test runs both Flows against local Dex without live provider credentials.
