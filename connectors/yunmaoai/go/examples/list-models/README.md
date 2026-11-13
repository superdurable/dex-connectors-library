# 云猫 model list example

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `yunmaoai` connection in Dex Web using a Key from the [云猫 console](https://api.yunmaoai.com/sign-up) 令牌 or API 密钥 page. Start `YunMaoAIModelCatalog` with `{}`. The default `oneModel` mock returns one example model ID; `noModels` returns an empty list. A real connection calls the fixed 云猫 HTTPS API.

Generate the strict FDG 2.0 graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/yunmaoai-models.fdg flow/workflow.go
```
