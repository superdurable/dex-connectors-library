# List JisuAPI provinces

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `jisuapi` connection in Dex Web using the `appkey` from [极速数据个人中心](https://www.jisuapi.com/my/) after applying for the administrative-area API. Start `JisuAPIProvinces` with `{}`. The default mock returns one province ID and name.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/jisuapi-provinces.fdg flow/workflow.go
```

Run deterministic tests from `connectors/jisuapi/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 10061 -web-port 10062 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./examples/list-provinces/flow -run TestListProvincesUsesDefaultMockWithRealDex -count=1`.
