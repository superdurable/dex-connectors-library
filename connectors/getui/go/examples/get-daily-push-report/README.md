# Read Getui daily push totals

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `getui` connection in Dex Web using appId, appKey, and MasterSecret from [个推开发者中心 > 配置管理 > 应用配置](https://docs.getui.com/getui/start/devcenter/). Start `GetuiDailyPushReport` with `{"date":"2021-01-01"}`. The default mock returns aggregate counts for that date.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/getui-daily-push-report.fdg flow/workflow.go
```

Run deterministic tests from `connectors/getui/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 10061 -web-port 10062 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./examples/get-daily-push-report/flow -run TestGetDailyPushReportUsesDefaultMockWithRealDex -count=1`.
