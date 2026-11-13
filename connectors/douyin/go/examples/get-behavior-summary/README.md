# Read Douyin mini app behavior summary

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `douyin` connection in Dex Web using AppID/client_key and AppSecret/client_secret from [小程序控制台 > 开发 > 开发配置](https://developer.open-douyin.com/docs/resource/zh-CN/mini-app/develop/tutorial/beginner/token/). Start `DouyinBehaviorSummary` with `{"date":"2026-11-11"}` for the mock, or replace the date with a completed day within the previous 90 days for a live query. The default mock returns aggregate activity counts.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/douyin-behavior-summary.fdg flow/workflow.go
```

Run deterministic tests from `connectors/douyin/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 10061 -web-port 10062 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./examples/get-behavior-summary/flow -run TestGetBehaviorSummaryUsesDefaultMockWithRealDex -count=1`.
