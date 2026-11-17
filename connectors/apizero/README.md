# ApiZero connector

This connector reads the current Key's [ApiZero account information](https://apizero.cn/aidocs/user-info/raw.md) with `GET https://v1.apizero.cn/api/user-info`. A standard Key returns its owner's wallet balance in yuan. A prepaid Key returns only that Key's remaining call quota; no wallet information enters Flow results. ApiZero is operated by [极数本源（福州）科技有限公司](https://apizero.cn/).

Sign in at [API Key](https://apizero.cn/account/keys), create or copy a secret `sk_live_` Key, and save it in Dex Web Connections. The Key is sent only in the Bearer header. The query takes `{}` and has no account ID. The provider documents a free, read-only request with a default one-request-per-second limit and a daily quota. Business code `0` is success. Every HTTP 429, including per-second throttling and daily-quota exhaustion, retries and honors `Retry-After`; Key rejection is terminal. The connector never copies raw provider messages into results or failures.

Run the [example Flow](go/examples/get-account-info/) with the generated mock or a user-controlled connection. The live provider response is unverified without a user-controlled Key.

## Python

The [Python package](python/README.md) implements `getAccountInfo` and includes
a runnable Dex Flow plus Go-recorded provider conformance scenarios.

The [TypeScript package and runnable Flow](typescript/README.md) replay the Go-recorded provider contract and exercise the Flow against a real local Dex Server.
