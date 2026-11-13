# Meiqia conversation read Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `meiqia` connection in Dex Web with the current Access Token from [设置 > 开发者 > APIs](https://www.meiqia.com/help/article/apiv2-0/). Start `MeiqiaConversationRead` with `{"conversationID":123}`. The default mock returns synthetic conversation metadata.

Generate and lint the strict FDG 2.0 graph:

```bash
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/meiqia-conversation-read.fdg flow/workflow.go
```
