# OpenCSG gateway model list example

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add an `opencsg` connection in Dex Web using an AI Gateway Key from [OpenCSG API Keys](https://opencsg.com/docs/profile/api_keys). Start `OpenCSGModelCatalog` with `{}`. The default `oneModel` mock returns one example model ID; `noModels` returns an empty list. A real connection reads only model IDs from the community AI Gateway.

Generate the strict FDG 2.0 graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/opencsg-models.fdg flow/workflow.go
```
