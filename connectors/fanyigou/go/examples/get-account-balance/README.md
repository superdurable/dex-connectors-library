# Fanyigou account balance Flow

Run `go run .` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Configure the static `fanyigou` Connection in Dex Web with the appid and privatekey from [Fanyigou Open Platform](https://open.fanyigou.com/filecore.html). Start `FanyigouAccountBalance` with `{}`. The default `balance` mock returns 137684 remaining pages.

Generate the strict Flow Definition Graph with `GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/fanyigou-account-balance.fdg flow/workflow.go`. Run the real Dex mock integration with `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./flow -run TestGetAccountBalanceUsesDefaultMockWithRealDex`.
