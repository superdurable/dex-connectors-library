# Getui connector

This connector uses [Getui REST API v2](https://docs.getui.com/getui/server/rest_v2/standard/) to read one application's [daily push report](https://docs.getui.com/getui/server/rest_v2/report/), `GET /report/push/date/{date}`. Only historical dates are supported. The output includes aggregate request, eligible, sent, received, displayed, and clicked counts; channel-level reports and raw messages are discarded.

In the [Getui developer center](https://dev.getui.com/), select or create a push app and open 配置管理 > 应用配置. Copy its appId, appKey, and MasterSecret into Dex Web Connections. The [official developer-center guide](https://docs.getui.com/getui/start/devcenter/) says phone verification is required to view MasterSecret. The connector signs `appKey + millisecond timestamp + MasterSecret` with SHA-256 for [POST /auth](https://docs.getui.com/getui/server/rest_v2/token/), caches the returned token in worker memory until expiry, and sends it only in the `token` header. Redirects are disabled and secrets are excluded from Flow output, receipts, and errors.

Use the [example Flow](go/examples/get-daily-push-report/) with the generated mock or a user-controlled connection. Getui limits auth calls to 100 per minute and 100,000 per day. A restart clears the in-memory token cache.
