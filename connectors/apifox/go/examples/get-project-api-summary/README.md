# Apifox project API summary example

Run `go run .` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add an `apifox` connection in Dex Web with a token from [avatar > 账号设置 > API 访问令牌](https://docs.apifox.com/api-access-token). Find the positive decimal project ID at 项目设置 > 基本设置. Start `ApifoxProjectAPICatalog` with `{"projectID":"123"}`, replacing the example ID with your project's ID. The default `oneEndpoint` mock returns a sample summary; `emptyProject` returns zero endpoints. A real connection exports the OpenAPI document and returns only its summary.

Generate the strict FDG 2.0 graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/apifox-project-summary.fdg flow/workflow.go
```
