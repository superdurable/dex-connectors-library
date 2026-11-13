# Juxingyi model catalog Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `huo15` connection in Dex Web with a `fsk-` API Key from [控制台 > API 密钥](https://fireworks-simulator.huo15.com/app/). Start `Huo15ModelCatalog` with `{}`. The default `oneModel` mock returns one example model ID; `noModels` returns an empty list. A real connection calls the fixed Juxingyi HTTPS API.

Generate and lint the strict FDG 2.0 graph:

```bash
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/huo15-model-catalog.fdg flow/workflow.go
```
