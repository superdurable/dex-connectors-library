# Tushare trading day Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `tushare` connection in Dex Web with the secret token from [个人中心 > 接口 TOKEN](https://tushare.pro/document/1?doc_id=40). Start `TushareTradingDay` with `{"exchange":"SSE","date":"20261113"}`. The default mock returns a synthetic exchange trading day.

Generate and lint the strict FDG 2.0 graph:

```bash
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/tushare-trading-day.fdg flow/workflow.go
```
