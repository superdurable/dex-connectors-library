# Worktile connector

This connector implements the current Worktile public-cloud [paged project query](https://worktile.apifox.cn/api-101771901). It obtains a short-lived tenant token from the fixed HTTPS [token endpoint](https://worktile.apifox.cn/api-101771895) for each call, then requests one page of projects. `PageIndex` starts at zero; `PageSize` is 1–100. The output includes project ID, name, code, visibility, and `HasMore`; it does not include member IDs or descriptions.

A Worktile administrator creates an app under **Management Backend > Application Management > My Apps** and copies its Client ID and Client Secret into Dex Web Connections. Tenant authorization grants team-wide access. The connector sends both credentials only to the HTTPS token endpoint and the resulting token only to the fixed HTTPS project endpoint. It does not place credentials, tokens, raw URLs, provider errors, or member IDs in Flow state. Worktile documents a [360-call-per-minute limit](https://worktile.apifox.cn/) and a 7200-second tenant token lifetime. The connector fetches a fresh token for each Query instead of persisting it.

The runnable [project-list example](go/examples/list-projects/README.md) demonstrates the generated operation-specific Step and static `worktile` connection. Real Worktile authorization remains to be tested with a user-controlled Dex Web Connection.
