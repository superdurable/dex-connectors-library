# XingyAPI managed account status

This connector checks whether an existing managed Enterprise WeChat instance is online through the [XingyAPI personal/getInfo API](https://api.xingyapi.com/api-docs). XingyAPI is operated by [郑州竣名信息科技有限公司](https://www.xingyapi.com/). The official [OpenAPI document](https://api.xingyapi.com/api-docs/download/openapi.json) specifies HTTPS POST, a JSON `instance_guid`, `X-Nebula-Key` authorization, and the `{ok,code,message,request_id,data}` response envelope.

Sign in at the [developer console](https://open.xingyapi.com), create or select a Key on **API Key**, and save it as a secret in the Dex Web `xingyapi` connection. Copy an existing `inst_xxxx` identifier from **实例**; the instance must already have completed the provider's scan login or console one-click login process. Start `GetInstanceOnlineStatus` with `{"instanceGUID":"inst_xxxxxxxx"}`. Leave timeout and maxResponseBytes blank to use the displayed defaults.

The Query returns only `isOnline` and the decimal `accountID` string when online. It discards the API's name, mobile, email, and other personal fields. A `code=50001` response with the documented no-account message becomes offline. Transient errors retry; authentication, permission, and malformed responses do not. The Key and provider message never enter Flow output or failures. Redirects are disabled.

Run the [instance online status Flow](go/examples/get-instance-online-status) with `dexcli dev`. The mock covers the typed Dex Step and persistence path. A live XingyAPI Key, managed instance, and account-specific permissions have not been verified.
