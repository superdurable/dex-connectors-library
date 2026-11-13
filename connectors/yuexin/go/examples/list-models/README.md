# Yuexin model list example

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `yuexin` connection in Dex Web using a Key from [Console > API Keys](https://app.yuexin.ai/keys). Start `YuexinModelCatalog` with `{}`. The default `oneModel` mock returns one example model ID; `noModels` returns an empty list. A real connection calls the fixed Yuexin HTTPS API.

Generate the strict FDG 2.0 graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/yuexin-models.fdg flow/workflow.go
```
