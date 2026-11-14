# Likelic room directory Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `likelic` connection in Dex Web with Customer Code and API Key from [外部拓展 > API 设置](https://agent.likelic.com/doc/v1/). Start `LikelicRoomCatalog` with `{"page":1}`. The default `oneRoom` mock returns one synthetic room; `noRooms` returns an empty page. A real connection calls the fixed Likelic HTTPS API.

Generate and lint the strict FDG 2.0 graph:

```bash
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/likelic-room-catalog.fdg flow/workflow.go
```
