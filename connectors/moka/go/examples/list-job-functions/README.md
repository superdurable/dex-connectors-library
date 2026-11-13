# List Moka job functions

Run `go run .` from this directory with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `moka` connection in Dex Web using a Moka API Key and organization ID. Start `MokaJobFunctionList` with `{}`; it returns job function IDs, names, and job counts. The example's default mock returns two functions without calling Moka.

Generate and lint a strict FDG 2.0 graph from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out - flow/workflow.go
```

Use `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/list-job-functions/flow -run TestListJobFunctionsUsesDefaultMockWithRealDex -count=1` with a local `dexcli dev` server for real Dex mock integration.
