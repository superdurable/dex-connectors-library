# FOFA domain asset search example

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `fofa` connection in Dex Web using a secret Key from [FOFA personal center > API information](https://fofa.info). Start `FOFADomainAssetSearch` with `{"domain":"example.com"}`. The default `oneHost` mock returns one example asset; `noHosts` returns an empty list.

Generate a strict Flow Definition Graph 2.0 from `flow/workflow.go`:

```bash
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/fofa-domain-assets.fdg flow/workflow.go
```

Run deterministic tests from `connectors/fofa/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 10061 -web-port 10062 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./examples/search-domain-assets/flow -run TestSearchDomainAssetsUsesDefaultMockWithRealDex -count=1`.
