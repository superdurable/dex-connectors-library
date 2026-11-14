# Yuandian company profile Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `yuandian` connection in Dex Web using a secret key from [个人中心 > API Key 管理](https://open.chineselaw.com/mcp-config). Start `YuandianCompanyProfile` with `{"creditCode":"91110108MA0074PN30"}`. The default mock returns synthetic registration facts; it does not call the real provider.

Generate and lint the strict FDG 2.0 graph:

```bash
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/yuandian-company-profile.fdg flow/workflow.go
```
