# XingyAPI instance online status Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `xingyapi` connection in Dex Web using a secret from the [developer console API Key page](https://open.xingyapi.com). Copy an existing `inst_xxxx` from the **实例** page. Start `XingyAPIInstanceOnlineStatus` with `{"instanceGUID":"inst_xxxxxxxx"}`. The default mock returns an online account ID.

Generate the strict FDG 2.0 graph from this runnable source:

```sh
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/xingyapi-instance-online-status.fdg flow/workflow.go
```

Provider authorization and a logged-in managed instance require a user-controlled live connection.
