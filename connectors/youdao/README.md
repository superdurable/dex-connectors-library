# Youdao Text Translation

The Go `translateText` Query posts up to 5,000 UTF-8 characters to the [Youdao Zhiyun Text Translation API](https://ai.youdao.com/DOCSIRMA/html/transapi/trans/api/wbfy/index.html). Its v3 signature is computed from the App Key, truncated input, fresh salt, UTC timestamp, and App Secret. Only the App Key and signature are transmitted; the App Secret stays in the Dex credential.

## Connect

Sign in at [Youdao Zhiyun](https://ai.youdao.com), open **Console > Application Management**, create or select a server API application, and bind the Text Translation service. Copy its App Key to `appKey` and paired secret to `appSecret` in Dex Web Connections. The latter is secret; blank values prevent calls. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for displayed defaults.

The operation accepts `text`, `sourceLanguage`, and `targetLanguage`. Use Youdao language codes such as `en` and `zh-CHS`; `auto` is valid only for the source. Responses retain just translation strings and requested language codes, omitting provider dictionary links and audio URLs. Provider error codes 411/412 and 302/303/304 retry; definitive rejection selects `providerRejected`; malformed or oversized JSON selects `invalidResponse`. The retry budget is three attempts over five minutes because repeat translations can incur another charge. Optional branches are unwired in the example and fail the Flow.

Run the [Text Translation example](go/examples/text-translation) with its static `youdao` connection. Provider tests use a local HTTP server. Live Youdao credentials and a real provider translation remain unverified.
