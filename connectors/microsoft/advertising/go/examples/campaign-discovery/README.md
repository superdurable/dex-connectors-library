# Microsoft Advertising campaign discovery

This Go Flow calls `microsoftadvertising.NewListCampaignsStep` for one numeric account ID and persists the bounded `CampaignsPage` in a Dex Attribute before completion. Its static Dex Web connection name is `microsoft-advertising`.

From the repository root, generate and lint the FDG 2.0 graph:

```sh
dexcli visualize connectors/microsoft/advertising/go/examples/campaign-discovery/flow/workflow.go --schema-version 2.0 --json --out /tmp/microsoft-advertising-fdg --lint app
```

Create a connection in Dex Web as described in [the connector README](../../../README.md), then run `go run ./examples/campaign-discovery` from `connectors/microsoft/advertising/go`. Start `MicrosoftAdvertisingCampaignDiscovery` with input `{"accountId":"12345"}`, where the ID is the `aid` value in the Microsoft Advertising Campaigns URL. The mock integration test runs the same Flow against local Dex without live provider credentials.
