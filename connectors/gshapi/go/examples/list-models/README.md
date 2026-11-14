# GSH API model catalog Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `gshapi` connection in Dex Web with a `sk-gsh-` API Key issued after [subscription](https://api.gshpay.com/pricing.html). Start `GSHAPIModelCatalog` with `{}`. The default `oneModel` mock returns one example model ID; `noModels` returns an empty list. A real connection calls the fixed GSH API HTTPS API.

Generate and lint the strict FDG 2.0 graph:

```bash
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/gshapi-model-catalog.fdg flow/workflow.go
```
