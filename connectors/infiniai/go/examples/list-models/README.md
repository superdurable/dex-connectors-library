# List GenStudio models

Start `dexcli dev -dex-port 9541 -web-port 9542 -open=false`, then run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9541` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add an `infiniai` connection in Dex Web using the `sk-` prefixed API key from **avatar → Access Control → API Key Management** in the [Infini AI console](https://cloud.infini-ai.com/). Start the `InfiniAIListModels` Flow with `{}`. Its default mock returns two models without contacting GenStudio.

Generate and lint a strict FDG 2.0 graph from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out flow/flow.fdg flow/workflow.go
```

Run `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/list-models/flow -run TestListModelsUsesDefaultMockWithRealDex -count=1` against a local `dexcli dev` server for real Dex mock integration.
