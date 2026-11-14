# Check an inbound IP with ThreatBook

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `threatbook` connection in Dex Web using a Key from [我的API > 我的KEY](https://x.threatbook.com/apiDocs/start/get_api_key). Start `ThreatBookIPReputation` with `{"ipAddress":"159.203.93.255"}`. The default mock returns a suspicious IP verdict.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/threatbook-ip-reputation.fdg flow/workflow.go
```

Run deterministic tests from `connectors/threatbook/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 10061 -web-port 10062 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./examples/check-ip-reputation/flow -run TestCheckIPReputationUsesDefaultMockWithRealDex -count=1`.
