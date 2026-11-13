# VAST Tripo account balance

The Go connector reads available and frozen Tripo API credits. VAST identifies [Beijing Sanqi Technology Co., Ltd.](https://www.vastworld.ai/trust) as its China-registered entity and Tripo API as its developer product. The [official account reference](https://developers.tripo3d.com/en/docs/account) documents `GET https://openapi.tripo3d.com/v3/account/balance`, Bearer API Key, and `code: 0` with numeric `data.balance` and `data.frozen`.

The connector outputs those numbers as exact decimal strings, preserving the provider's credit precision. It never creates generation tasks or reads usage history. The [authentication guide](https://developers.tripo3d.com/en/docs/authentication) locates one-time-shown keys in **Tripo Console > API Keys > Create Key**. The [error guide](https://developers.tripo3d.com/en/docs/error-handling) defines authentication, permission, rate, and service errors; the connector retries only documented transient failures.

See the [runnable Dex Flow](go/examples/account-balance/README.md). Mock tests do not verify a live key, account balance, or provider responses.
