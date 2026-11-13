# StarHub Ai model catalog Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `starhub` connection in Dex Web with a `sk-` API Key from [控制台 > API Keys](https://www.starhubc.com). Start `StarHubModelCatalog` with `{}`. The default `oneModel` mock returns one example model ID; `noModels` returns an empty list. A real connection calls the fixed StarHub Ai HTTPS API.

Generate and lint the strict FDG 2.0 graph:

```bash
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/starhub-model-catalog.fdg flow/workflow.go
```
