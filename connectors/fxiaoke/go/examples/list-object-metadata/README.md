# List FXiaoke CRM object metadata

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add an `fxiaoke` connection in Dex Web using the enterprise app credentials, cloud host, and service employee ID. Start `FXiaokeObjectMetadata` with `{}`. The default mock returns the `AccountObj` metadata without a provider call.

Generate and lint strict FDG 2.0 from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out flow/flow.fdg flow/workflow.go
```

Use `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/list-object-metadata/flow -run TestListObjectMetadataUsesDefaultMockWithRealDex -count=1` with a local `dexcli dev` server for real Dex mock integration.
