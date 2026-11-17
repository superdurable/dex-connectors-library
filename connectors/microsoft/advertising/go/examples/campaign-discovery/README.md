# Microsoft Advertising campaign discovery

This Go example registers two Flows. `MicrosoftAdvertisingCampaignDiscovery` lists an account's campaigns; `CampaignInspectionFlow` calls `microsoftadvertising.NewGetCampaignStep` for one campaign ID and stores its summary in a Dex Attribute. Both use the static Dex Web connection name `microsoft-advertising`.

From the repository root, generate and lint the FDG 2.0 graph:

```sh
dexcli visualize connectors/microsoft/advertising/go/examples/campaign-discovery/flow/workflow.go --schema-version 2.0 --json --out /tmp/microsoft-advertising-fdg --lint app
dexcli visualize connectors/microsoft/advertising/go/examples/campaign-discovery/flow/campaign_inspection.go --schema-version 2.0 --json --out /tmp/microsoft-advertising-inspection-fdg --lint app
```

Create a connection in Dex Web as described in [the connector README](../../../README.md), then run `go run ./examples/campaign-discovery` from `connectors/microsoft/advertising/go`. Start `MicrosoftAdvertisingCampaignDiscovery` with input `{"accountId":"12345"}`, where the ID is the `aid` value in the Microsoft Advertising Campaigns URL. Start `CampaignInspectionFlow` with `{"accountId":"12345","campaignId":"98765"}` to inspect one campaign. The mock integration test runs both Flows against local Dex without live provider credentials.
