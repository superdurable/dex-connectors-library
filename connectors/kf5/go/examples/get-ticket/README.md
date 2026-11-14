# KF5 ticket read Flow

Run `go run .` here with `DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev` and Dex project configuration available to `projectconfig.LoadFromEnvironment`. Add a `kf5` connection in Dex Web with the tenant subdomain, agent email, and API token from [系统设置 > 支持渠道 > 开放API设置](https://developer.kf5.com/doc/restapi/helpcenter/). Start `KF5TicketRead` with `{"ticketID":123}`. The default mock returns synthetic ticket status metadata.

Generate and lint the strict FDG 2.0 graph:

```bash
GOWORK=off dexcli visualize -json -schema-version 2.0 -lint app -out /tmp/kf5-ticket-read.fdg flow/workflow.go
```
