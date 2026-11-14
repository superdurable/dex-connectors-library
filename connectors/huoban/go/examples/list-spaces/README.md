# List Huoban workspaces

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `huoban` connection in Dex Web using an API Key from [伙伴云企业后台 > 接口授权](https://app.huoban.com/v5). Start `HuobanSpaces` with `{}`. The default mock returns one workspace ID and name. The Flow does not read table records.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/huoban-spaces.fdg flow/workflow.go
```

Run deterministic tests from `connectors/huoban/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 9981 -web-port 9982 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9981 GOWORK=off go test -tags=integration ./examples/list-spaces/flow -run TestListSpacesUsesDefaultMockWithRealDex -count=1`.
