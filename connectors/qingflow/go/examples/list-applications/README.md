# List Qingflow applications

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `qingflow` connection in Dex Web using a super-admin `accessToken` from [轻流 OPEN API configuration](https://qingflow.com/). Start `QingflowApplications` with `{}`. The default mock returns one appKey and appName. The Flow does not read application records.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/qingflow-applications.fdg flow/workflow.go
```

Run deterministic tests from `connectors/qingflow/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 9991 -web-port 9992 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9991 GOWORK=off go test -tags=integration ./examples/list-applications/flow -run TestListApplicationsUsesDefaultMockWithRealDex -count=1`.
