# List PPIO models

Run `go run .` from this directory with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `ppio` connection in Dex Web using a PPIO API key. Start `PPIOModelList` with `{}`; it returns the models visible to that key. The example's default mock returns two models without calling PPIO.

Generate and lint a strict FDG 2.0 graph from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out flow/flow.fdg flow/workflow.go
```

Use `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/list-models/flow -run TestListModelsUsesDefaultMockWithRealDex -count=1` with a local `dexcli dev` server for real Dex mock integration.
