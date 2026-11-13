# List Zilliz Cloud clusters

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `zilliz` connection in Dex Web using a Key from [organization > API Keys](https://docs.zilliz.com/docs/manage-api-keys). Start `ZillizClusterCatalog` with `{}`. The default `oneCluster` mock returns one example cluster; `noClusters` returns an empty list.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/zilliz-clusters.fdg flow/workflow.go
```

Run deterministic tests from `connectors/zilliz/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 10061 -web-port 10062 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./examples/list-clusters/flow -run TestListClustersUsesDefaultMockWithRealDex -count=1`.
