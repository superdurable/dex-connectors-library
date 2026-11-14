# iFLYTEK Spark Chat

The Go `createChatCompletion` Query sends one bounded, text-only, nonstreaming request to the [iFLYTEK Spark HTTP API](https://www.xfyun.cn/doc/spark/HTTP%E8%B0%83%E7%94%A8%E6%96%87%E6%A1%A3.html). The connection defaults to the documented `lite` model and a 512-token output limit. It rejects retired Max model IDs. Input is limited to 2,000 UTF-8 characters; responses are bounded to the configured byte limit.

## Connect

Sign in to the [iFLYTEK console](https://console.xfyun.cn), open **My Services > Spark**, enable the model version you will use, and find **HTTP service authentication information** for that version. Copy its opaque APIPassword to the secret `apiPassword` field in Dex Web Connections. Dex sends it as a Bearer token only over HTTPS. Blank prevents calls. Leave `baseUrl`, `timeout`, `maxResponseBytes`, `model`, and `maxOutputTokens` blank for displayed defaults. To use Pro or Ultra, choose a model enabled for that APIPassword.

The operation accepts a `prompt` and returns assistant `content`, the selected model ID, and provider token usage. HTTP 429/5xx and provider rate codes 10007/11202/11203 retry; definite provider rejection selects optional `providerRejected`; malformed or oversized responses select optional `invalidResponse`. The retry budget is three attempts over five minutes because a repeated generation can be charged again. Execute uses sync durability because model generation may exceed seven seconds. Optional branches are unwired in the example and fail the Flow.

Run the [Spark Chat example](go/examples/spark-chat) with its static `iflytek` connection. Provider tests use a local HTTP server. A real Spark generation with user credentials, authorization for Lite/Pro/Ultra, and actual token billing remain unverified.
