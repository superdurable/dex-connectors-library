# Read iP138 token usage

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add an `ip138` connection in Dex Web using the token obtained through [iP查询 > 在线API申请](https://user.ip138.com/ip/). Start `iP138TokenUsage` with `{}`. The default mock returns current-hour and package quota counts.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/ip138-token-usage.fdg flow/workflow.go
```

Run deterministic tests from `connectors/ip138/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 10061 -web-port 10062 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./examples/get-token-usage/flow -run TestGetTokenUsageUsesDefaultMockWithRealDex -count=1`.
