# SenseNova model list example

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `sensetime` connection in Dex Web using an API Key from [ModelStudio Management Center](https://console.sensecore.cn). Start `SenseTimeModelCatalog` with `{}`. The default `oneModel` mock returns one example model ID; `noModels` returns an empty list. A real connection calls the fixed SenseNova HTTPS API.

Generate the strict FDG 2.0 graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/sensetime-models.fdg flow/workflow.go
```
