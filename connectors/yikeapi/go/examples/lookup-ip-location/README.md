# YikeAPI IP location Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `yikeapi` connection in Dex Web with `appid` and `appsecret` from [用户中心](https://www.yikeapi.com/index/ip) after activating IP地址归属地查询. Start `YIKEAPILocationLookup` with `{"ipAddress":"8.8.8.8"}`. The default mock returns a synthetic Shenzhen location.

Generate and lint the strict FDG 2.0 graph:

```bash
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/yikeapi-location.fdg flow/workflow.go
```
