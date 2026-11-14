# Zhihuiya patent valuation Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `zhihuiya` connection in Dex Web with the secret API Key from the [management console](https://developers.zhihuiya.com/devportal/guides/authentication). Start `ZhihuiyaPatentValuation` with `{"publicationNumber":"JP6012277B2"}`. The default mock returns synthetic valuation metrics.

Generate and lint the strict FDG 2.0 graph:

```bash
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/zhihuiya-patent-valuation.fdg flow/workflow.go
```
