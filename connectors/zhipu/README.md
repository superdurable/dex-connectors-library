# Zhipu AI connector

This Go connector calls [Zhipu's Chat Completions API](https://docs.bigmodel.cn/api-reference/%E6%A8%A1%E5%9E%8B-api/%E5%AF%B9%E8%AF%9D%E8%A1%A5%E5%85%A8) at `POST https://open.bigmodel.cn/api/paas/v4/chat/completions`. `generateText` sends one user message, sets `stream: false`, requests text output, and sends no tools. The default model is `glm-4.7-flash`. The operation caps the prompt at 64 KiB, output at 4,096 tokens, and response at the configured byte limit (2 MiB by default). A zero `maxOutputTokens` uses 1,024.

## Connection

Open [BigModel API Keys](https://bigmodel.cn/usercenter/proj-mgmt/apikeys), select the project that owns usage, create a key, and save it as the Dex `api_key` secret. The key is sent only in the Authorization bearer header. Leave `model`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. `model` may be any text model ID your project can use from the linked API reference. Revoke or rotate the key on the same BigModel page.

## Go

The [text generation example](go/examples/text-generation/) is a runnable Worker. Its Flow uses the static Dex Web connection name `zhipu` and `zhipu.NewGenerateTextStep`. `generated` completes the Flow; `truncated`, `blocked`, `providerRejected`, `invalidResponse`, and `defect` are optional branches and fail the Flow unless an application wires a business response. Transient failures retry up to three times under a one-hour budget. A repeated generation can be billed again.

No live BigModel API key is bundled or required for mock verification. A real provider call requires saving your own connection in Dex Web.
