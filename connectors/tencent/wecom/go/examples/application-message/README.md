# WeCom application message Flow

This runnable Flow calls `sendApplicationMessage` with the static `wecom-app`
connection and completes with WeCom's msgid and the recipients it skipped.
Optional branches, including `uncertain`, are unwired so they fail the Flow.
WeCom duplicate checking makes every retry safe within four hours, so the
default one-hour retry policy never reaches `uncertain`; only a retry 3 hours
50 minutes or more after the first attempt selects it.

## Strict FDG 2.0 generation

From `connectors/tencent/wecom/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/application-message/flow/workflow.go --schema-version 2.0 --json --out ./build/application-message
```

The command must report `valid: true`. An unpublished connector can report
`connector_release_required` until its release artifact exists.

## Run with Dex Web

Create the connector release artifact, pass it to `dexcli dev` with
`--connector-release-override wecom=<artifact-directory>`, and use the
generated Flow rendering. In Dex Web Connections, select `wecom / wecom-app`,
choose the **Self-built application** sender, and follow its guide to copy the
企业ID, AgentId, and Secret from the
[WeCom admin console](https://work.weixin.qq.com/wework_admin/frame#apps). Add
the Worker's public egress IP under the application's 企业可信IP (Trusted
Company IP), and include every recipient in its 可见范围 (Visible Range). Leave
base URL, timeout, and response limit blank for their defaults.

With `DEX_PROJECT_*` configured, run from `connectors/tencent/wecom/go`:

```bash
go run ./examples/application-message
```

In Dex Web Start Flow, choose `WeComApplicationMessage`, enter a unique Flow
ID, and use:

```json
{"toUserIds":["zhangsan"],"messageType":"text","content":"Incident INC-7 is open"}
```

Replace `zhangsan` with a member's account ID from the admin console
通讯录 (Contacts), or use `toDepartmentIds`, `toTagIds`, or
`"toAllMembers":true`. Dex Web summary and display show the msgid and skipped
recipients, not the content. The real-Dex integration tests run with
`GOWORK=off go test -tags=integration ./examples/application-message/flow`
while `dexcli dev` runs.
