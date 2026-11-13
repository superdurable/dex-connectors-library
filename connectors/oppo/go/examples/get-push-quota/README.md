# OPPO Push daily quota Flow

Run `go run .` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Configure the static `oppo` Connection in Dex Web with AppKey and MasterSecret from [OPPO Push](https://push.oppo.com/) > Applications > your application. Start `OppoPushQuota` with `{}`. The default `quota` mock returns 100000 total, 10000 sent, and 90000 remaining messages. A real connection exchanges signed app credentials for a short-lived auth token and reads today's counters.

Generate the strict Flow Definition Graph with `GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/oppo-push-quota.fdg flow/workflow.go`. Run its real Dex mock integration with `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./flow -run TestGetPushQuotaUsesDefaultMockWithRealDex`.
