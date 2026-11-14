# 360 智脑 connector

This Go connector calls the [official Chat Completions API](https://ai.360.com/docs/413291990e0) at `POST https://api.360.cn/v1/chat/completions`. `generateText` sends one user message with `stream: false` and no tools. The default model is [`360zhinao/360zhinao-pro`](https://ai.360.com/open/zh/models/360zhinao/360zhinao-pro). This model's published rates are ¥2 per million input tokens and ¥5 per million output tokens; confirm the current rate and your account access in the model square before live use. The operation caps the prompt at 64 KiB, output at 4,096 tokens, and response at the configured byte limit (2 MiB by default). Zero `maxOutputTokens` uses 1,024.

## Connection

Sign in to [360 智脑 API Key management](https://ai.360.com/open/zh/platform/account/keys), select the organization that should own usage, create or select an API key, and save its opaque value as the Dex `api_key` secret. The key is sent only in the Authorization Bearer header. Leave `model`, `timeout`, and `maxResponseBytes` blank for their displayed defaults; a model override must be an ID available to your organization in the [model square](https://ai.360.com/open/zh/models). Revoke or rotate the key on the same API Key page.

## Go

The [text generation example](go/examples/text-generation/) is a runnable Worker with the static Dex Web connection name `qihoo360`. `generated` completes the Flow; `truncated`, `blocked`, `providerRejected`, `invalidResponse`, and `defect` are optional branches and fail the Flow unless an application wires a business response. HTTP 429, 408, and 5xx retry at most three attempts. Generation may be billed before a response reaches Dex, so repeating it may add cost. The example stores the model answer and usage, but does not log the prompt or API key. Use a nonsensitive prompt because Dex persists Flow start input.

No live API key is bundled. The local integration test uses the manifest's sample mock response; real 360 智脑 authorization and model generation remain unverified without a user-controlled connection in Dex Web.
