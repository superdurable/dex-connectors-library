# Kimi model list example

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `kimi` connection in Dex Web using a Key from [Console > API Keys](https://platform.kimi.com/console/api-keys). Start `KimiModelCatalog` with `{}`. The default `oneModel` mock returns one example model ID; `noModels` returns an empty list. A real connection calls the China-region Kimi API.

Generate the strict FDG 2.0 graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/kimi-models.fdg flow/workflow.go
```
