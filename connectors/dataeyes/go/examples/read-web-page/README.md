# Read a public page with DataEyes AI

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `dataeyes` connection in Dex Web with a search/reading Key from [MCP产品 → 接入与密钥](https://doc.shuyanai.com/doc-8441886). Start `DataEyesWebPageRead` with `{"url":"https://example.com"}`. The default mock returns a short Markdown page.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/dataeyes-web-page-read.fdg flow/workflow.go
```

Run deterministic tests from `connectors/dataeyes/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 10061 -web-port 10062 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./examples/read-web-page/flow -run TestReadWebPageUsesDefaultMockWithRealDex -count=1`.
