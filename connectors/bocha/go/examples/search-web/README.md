# Search the public web with Bocha

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `bocha` connection in Dex Web using a secret Key from [API KEY](https://open.bocha.cn/api-keys). Start `BochaWebSearch` with `{"query":"博查 搜索 API"}`. The default mock returns one example page; `noResults` returns an empty list.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/bocha-web-search.fdg flow/workflow.go
```

Run deterministic tests from `connectors/bocha/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 10061 -web-port 10062 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./examples/search-web/flow -run TestSearchWebUsesDefaultMockWithRealDex -count=1`.
