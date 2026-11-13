# 365 API model catalog Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `hby365` connection in Dex Web with an API Key API Key from [API Keys in the console](https://www.hby365.studio/console). Start `HBY365ModelCatalog` with `{}`. The default `oneModel` mock returns one example model ID; `noModels` returns an empty list. A real connection calls the fixed 365 API HTTPS API.

Generate and lint the strict FDG 2.0 graph:

```bash
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/hby365-model-catalog.fdg flow/workflow.go
```
