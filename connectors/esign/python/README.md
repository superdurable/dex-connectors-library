# eSign Python connector

`dex-connector-esign` provides the `getSignFlowStatus` Query for one existing signing flow. It signs a `GET /v3/sign-flow/{signFlowId}/detail` request with the AppSecret using eSign's [HmacSHA256 request signature](https://open.esign.cn/doc/opendoc/dev-guide3/tggw2e). Results contain only the requested flow ID and numeric status; signer details, document content, provider messages, and the AppSecret do not enter Flow output or failures.

## Connection

At the [eSign developer console](https://open.esign.cn/my-apps/home), open **正式服务 → 应用管理 → 我的应用**, select your production application, and copy its AppID into `app_id` and secret AppSecret into `app_secret` in Dex Connections. Blank credentials prevent requests. The connector uses the fixed production HTTPS host and does not fetch OAuthToken. Leave `timeout` (Go duration) and `maxResponseBytes` (bytes) blank for displayed defaults. There are no operation fields. Supply an existing opaque `signFlowId` from your signing workflow or flow list.

See `examples/get_sign_flow_status/` for a runnable Dex Flow and Worker. Run `uv run pytest tests` to replay Go-recorded signed HTTP scenarios. Live eSign authorization and signing status were not verified without a user-controlled connection and paid plan access.
