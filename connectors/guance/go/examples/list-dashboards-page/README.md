# List Guance dashboards

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `guance` connection in Dex Web using an API Key from **管理 > API Key 管理 > 新建 Key**. Start `GuanceDashboardsPage` with `{}` for page 1 of size 10, or `{"pageIndex":2,"pageSize":20}` for a later page. Page size must be at most 100. The default mock returns one dashboard.

Generate and lint strict FDG 2.0 from the runnable source:

```sh
dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/guance-flow.fdg flow/workflow.go
```

Use `GOWORK=off go test ./...` for deterministic checks. Run `GOWORK=off go test -tags=integration ./examples/list-dashboards-page/flow -run TestListDashboardsPageUsesDefaultMockWithRealDex -count=1` with a local `dexcli dev` server for real Dex mock integration.
