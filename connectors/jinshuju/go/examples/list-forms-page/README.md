# List Jinshuju forms

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `jinshuju` connection in Dex Web using a Jinshuju Access Token. Start `JinshujuFormsPage` with `{}` for the first page; pass `{"next":"<prior-page-next>"}` to continue. The cursor is non-secret and must be copied verbatim. Each page has up to 50 forms. The default mock returns one form.

Generate and lint strict FDG 2.0 from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out flow/flow.fdg flow/workflow.go
```

Use `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/list-forms-page/flow -run TestListFormsPageUsesDefaultMockWithRealDex -count=1` with a local `dexcli dev` server for real Dex mock integration.
