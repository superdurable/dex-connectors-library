# Doc2X PDF parse status Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `doc2x` connection in Dex Web with a platform API Key from [Doc2X Open Platform](https://open.noedgeai.com). Start `Doc2XParseStatus` with `{"uid":"TASK_UID"}`, replacing `TASK_UID` with the value returned by PDF preupload. The default `processing` mock returns 50% progress; `completed` returns success. A real connection calls the fixed Doc2X HTTPS status API.

Generate and lint the strict FDG 2.0 graph:

```bash
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/doc2x-parse-status.fdg flow/workflow.go
```
