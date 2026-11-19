# Doc2X connector

`GetParseStatus` reads `status` and `progress` for an existing Doc2X PDF parse task through [the official PDF API](https://doc2x.noedgeai.com/help/zh-cn/api/doc2x-api-v2-pdf-interface.html). [Doc2X Open Platform](https://open.noedgeai.com/login?intent=api) identifies its operator as 武汉智识无垠科技有限公司. This Query does not submit a document or consume parse credits. It omits PDF text, result URLs and failure details from its output.

Sign in at [Doc2X Open Platform](https://open.noedgeai.com), open **API Keys**, create or select a platform API Key, and copy it into Dex Web Connections. Use the `uid` returned by the PDF preupload API. A blank Key or uid blocks the request. Leave timeout and maxResponseBytes blank for their displayed defaults.

The checked-in [runnable parse status Flow](go/examples/get-parse-status/) uses `ConnectionName = "doc2x"`. Run the worker with `go run .` from its directory, then start `Doc2XParseStatus` with `{"uid":"TASK_UID"}`. Strict FDG 2.0 instructions and a Dex mock integration test are alongside the Flow.

The [Python parse status Flow](python/examples/get_parse_status/) uses the same static connection. Its package replays the Go-recorded provider scenarios, and the example verifies retry and filtered output through a real Dex Worker and Client.

Provider contract: [PDF API reference](https://doc2x.noedgeai.com/help/zh-cn/api/doc2x-api-v2-pdf-interface.html). Real provider authorization and status responses are unverified without a customer-controlled Key and existing task.
