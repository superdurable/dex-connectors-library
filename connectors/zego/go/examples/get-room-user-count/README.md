# Read a ZEGO room member count

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and a Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `zego` connection in Dex Web using AppId and ServerSecret from [ZEGO Console > 项目管理 > 查看 > 项目配置](https://doc-zh.zego.im/zim-server/accessing-server-apis). Start `ZEGORoomUserCount` with `{"roomId":"room-1"}` using a room ID assigned by your ZIM application. The default mock returns count 2 and no user identities.

Generate the strict Flow Definition Graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/zego-room-user-count.fdg flow/workflow.go
```

Run deterministic tests from `connectors/zego/go` with `GOWORK=off go test ./...`. For a real Dex worker with the generated mock, start `dexcli dev -dex-port 10061 -web-port 10062 -open=false`, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:10061 GOWORK=off go test -tags=integration ./examples/get-room-user-count/flow -run TestGetRoomUserCountUsesDefaultMockWithRealDex -count=1`.
