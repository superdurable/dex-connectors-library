# Wapi vehicle brand Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `wapi` connection in Dex Web using AppID and AppSecret from [我的应用](https://www.wapi.cn). Start `WapiVehicleBrands` with `{}`. The default `oneBrand` mock returns one synthetic brand and price; `noBrands` returns an empty list. A real connection calls Wapi's fixed HTTPS brand-price API.

Generate and lint the strict FDG 2.0 graph:

```bash
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/wapi-vehicle-brands.fdg flow/workflow.go
```
