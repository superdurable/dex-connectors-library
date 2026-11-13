# CTYun Knowledge Base Q&A connector

This connector lists resource metadata from [CTYun Knowledge Base Q&A](https://www.ctyun.cn/document/11032279/11084675). It returns knowledge-base ID, name, and file count; it does not retrieve documents. CTYun's official site identifies the operator as 天翼云科技有限公司.

## Connection

In the [CTYun account center](https://www.ctyun.cn/), open **安全设置 > 登录保护 > 用户AccessKey > 新建** and copy AK and SK into the secret connection fields. In Knowledge Base Q&A console open **开始使用 > 访问**; the dialogue page shows the required `tenantId` ([official location guide](https://www.ctyun.cn/document/11032279/11074496)). The credentials must have access to that tenant. Leave transport settings blank for their documented defaults.

The connector signs each GET request with [CTYun EOP authentication](https://www.ctyun.cn/document/11032279/11075051), using a fresh UUID and Beijing-local `eop-date`. It never puts AK/SK, tenant ID, or signature in the Flow result. It retries transport, 429, 5xx, and the documented temporary `40002`/`40004` failures. Other provider business errors use optional branches; an unwired optional branch fails the Flow.

The [runnable Dex Flow](go/examples/knowledge-base-directory/README.md) uses the static `ctyun` connection and a synthetic mock. Live provider authorization and tenant access require user-controlled CTYun credentials and have not been verified here.
