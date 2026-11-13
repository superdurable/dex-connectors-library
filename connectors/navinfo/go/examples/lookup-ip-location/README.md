# NavInfo IP location example

Run `go run .` with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `navinfo` connection in Dex Web using a REST API AK. Find the AK application link beside the AK parameter in the [IP定位 documentation](https://lbs.navinfo.com/docs/webapi/ipcity.html). Start `NavInfoLocationLookup` with `{"ipAddress":"8.8.8.8"}`. The default `beijing` mock returns a coarse city result.

Generate the strict FDG 2.0 graph:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/navinfo-ip-location.fdg flow/workflow.go
```
