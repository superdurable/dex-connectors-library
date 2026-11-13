# Read ApiZero account information

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add an `apizero` connection in Dex Web using a Key from [Console > API Key](https://apizero.cn/account/keys). Start `ApiZeroAccountInfo` with `{}`. The default mock returns a standard account balance of 123.4000 yuan; the `prepaidKey` mock returns 42 remaining calls.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/apizero-account-info.fdg flow/workflow.go
```

Run deterministic tests from `connectors/apizero/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 10061 -web-port 10062 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./examples/get-account-info/flow -run TestGetAccountInfoUsesDefaultMockWithRealDex -count=1`.
