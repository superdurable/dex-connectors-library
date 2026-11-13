# Read an eSign signing-flow status

Run `go run .` in this directory with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Configure the `esign` connection in Dex Web with the AppID and AppSecret from [eSign 应用管理 > 我的应用](https://open.esign.cn/my-apps/home). Start `EsignSignFlowStatus` with `{"signFlowId":"<existing-flow-id>"}`. The default mock accepts `flow_example` and returns numeric status `1`. The Flow stores only that ID and status.

Generate the strict Flow Definition Graph from the Flow package:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/esign-sign-flow-status.fdg flow/workflow.go
```

Run deterministic provider and Flow tests from the connector's `go/` directory with `GOWORK=off go test ./...`. To test the mock against a real Dex worker, start `dexcli dev` and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:9911 GOWORK=off go test -tags=integration ./examples/get-sign-flow-status/flow -run TestGetSignFlowStatusUsesDefaultMockWithRealDex -count=1`.
