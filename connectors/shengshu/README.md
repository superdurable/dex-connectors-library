# Shengshu Technology Vidu connector

This connector implements the read-only [Vidu task detail API](https://platform.vidu.cn/docs/api-reference/task-management/task-detail). It sends the Vidu API key as `Authorization: Token <key>` to `GET https://api.vidu.cn/ent/v2/tasks/{id}/creations`. A task ID comes from an earlier Vidu generation request. The connector does not create a task or spend credits.

The response includes task state, error code, consumed credits, and creation IDs. Vidu also returns arbitrary payload and 24-hour media URLs that may contain signed access tokens; the connector deliberately omits those from durable output, receipts, Attributes, and logs. A `failed` task state is returned as a valid observed status. Missing task IDs select `notFound`; rate limits and temporary service errors retry.

Create an API key under [Vidu API Key 管理](https://platform.vidu.cn/) and enter it in Dex Web Connections without the `Token` prefix. The runnable [get-task-status example](go/examples/get-task-status/README.md) demonstrates the generated operation-specific Step and static `shengshu` connection. Real Vidu authorization remains to be tested with a user-controlled Dex Web Connection.
