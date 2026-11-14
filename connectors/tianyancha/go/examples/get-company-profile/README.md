# Tianyancha company profile Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `tianyancha` connection in Dex Web using the token from [数据中心 > 我的接口](https://open.tianyancha.com/open/365) after applying for interface 365. Start `TianyanchaCompanyProfile` with `{"keyword":"平安银行股份有限公司"}`. The default mock returns limited company registration facts.

Generate the strict FDG 2.0 graph from this runnable source:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/tianyancha-company-profile.fdg flow/workflow.go
```

Provider authorization and current paid-plan behavior require a user-controlled live connection.
