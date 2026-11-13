# Look up TianAPI holiday status

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `tianapi` connection in Dex Web using the secret key from [TianAPI 控制台 > 数据管理 > 我的密钥key](https://www.tianapi.com/demo/api/jiejiari.html) after applying for 节假日. Start `TianAPIHolidayStatus` with `{"date":"2021-01-01"}`. The default mock returns day code 1 and rest indicator 1 for that date.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/tianapi-holiday-status.fdg flow/workflow.go
```

Run deterministic tests from `connectors/tianapi/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 10061 -web-port 10062 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./examples/get-holiday-status/flow -run TestGetHolidayStatusUsesDefaultMockWithRealDex -count=1`.
