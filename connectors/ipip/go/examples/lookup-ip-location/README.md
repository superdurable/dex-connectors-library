# Look up an IPIP location

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add an `ipip` connection in Dex Web using an online query token obtained through [IPIP](https://user.ipip.net/) or its [sales contact](https://www.ipip.net/contact.html). Start `IPIPLocationLookup` with `{"ipAddress":"118.28.8.8"}`. The default mock returns coarse country, region, city, and country code fields.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/ipip-ip-location.fdg flow/workflow.go
```

Run deterministic tests from `connectors/ipip/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 10061 -web-port 10062 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./examples/lookup-ip-location/flow -run TestLookupIPLocationUsesDefaultMockWithRealDex -count=1`.
