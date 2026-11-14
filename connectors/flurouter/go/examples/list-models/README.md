# List FluRouter models

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `flurouter` connection in Dex Web using a Key from [Console > API Keys](https://flurouter.net/guide). Start `FluRouterModels` with `{}`. The default mock returns two sample model IDs; the `noModels` mock returns an empty list.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/flurouter-models.fdg flow/workflow.go
```

Run deterministic tests from `connectors/flurouter/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 10061 -web-port 10062 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./examples/list-models/flow -run TestListModelsUsesDefaultMockWithRealDex -count=1`.
