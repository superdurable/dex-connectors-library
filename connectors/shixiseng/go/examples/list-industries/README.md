# Shixiseng industry list Flow

Run `go run .` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add the static `shixiseng` Connection in Dex Web using APP_ID and APP_SECRET from [Shixiseng business onboarding](https://open.shixiseng.com/), and ask it to allowlist the Worker's outbound IP. Start `ShixisengIndustries` with `{}`. The default `industries` mock returns two industry labels.

Generate the strict Flow Definition Graph with `GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/shixiseng-industries.fdg flow/workflow.go`. Run the real Dex mock integration with `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./flow -run TestListIndustriesUsesDefaultMockWithRealDex`.
