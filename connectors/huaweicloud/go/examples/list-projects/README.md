# List accessible Huawei Cloud IAM projects

Run `go run .` from this directory with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `huaweicloud` connection in Dex Web using an IAM user's account name, username, and password. Start `Huawei CloudProjectList` with `{}`; it returns accessible project IDs, names, enabled flags, and domain IDs. The example's default mock returns two projects without calling Huawei Cloud.

Generate a strict FDG 2.0 graph from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -out flow/flow.fdg flow/workflow.go
```

Use `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/list-projects/flow -run TestListProjectsUsesDefaultMockWithRealDex -count=1` with a local `dexcli dev` server for real Dex mock integration.
