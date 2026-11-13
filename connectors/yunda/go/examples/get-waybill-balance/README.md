# Yunda electronic waybill balance Flow

Run `go run .` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Configure the static `yunda` Connection in Dex Web with the application app-key/app-secret and Baima partner ID/integration secret from [Yunda Open Platform](https://open.yundaex.com/) and your outlet. Start `YundaWaybillBalance` with `{}`. The default `balance` mock returns 1 free and 2 paid waybills.

Generate the strict Flow Definition Graph with `GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/yunda-waybill-balance.fdg flow/workflow.go`. Run the real Dex mock integration with `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./flow -run TestGetWaybillBalanceUsesDefaultMockWithRealDex`.
