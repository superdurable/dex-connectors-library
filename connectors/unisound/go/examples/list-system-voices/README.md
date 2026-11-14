# List Unisound system voices

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `unisound` connection in Dex Web using a secret Key from [控制台 > 按量计费 > API Key 管理](https://maas.unisound.com/docs/guide/quickstart). Start `UnisoundSystemVoices` with `{}`. The default `systemVoices` mock returns one voice; `noVoices` returns an empty list.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/unisound-system-voices.fdg flow/workflow.go
```

Run deterministic tests from `connectors/unisound/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 10061 -web-port 10062 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./examples/list-system-voices/flow -run TestListSystemVoicesUsesDefaultMockWithRealDex -count=1`.
