# UPYUN connector

The Go `getServiceUsage` Query reads the number of bytes stored by one UPYUN cloud storage service through the official [`GET /<bucket>/?usage` REST API](https://help.upyun.com/knowledge-base/rest_api/). It is read only. Requests use the [UPYUN HMAC-SHA1 Authorization scheme](https://help.upyun.com/knowledge-base/object_storage_authorization/) with the operator password's lowercase MD5 digest as key, an RFC 1123 GMT date, and the complete request URI including `?usage`. The plaintext password stays in the connection credential and is never put in Flow data.

## Connect

Sign in at the [UPYUN console](https://console.upyun.com). Under **Cloud Products → Cloud Storage → Service List**, create or select a service and copy its bucket name into `bucket`. In the service's **Configuration → Storage Management → Operator Authorization**, create or authorize an operator with **Read** permission. Alternatively, manage operators under **Account → Account Management → Operator Management**. Copy its nonsecret name to `operator` and its exact plaintext secret to `password`; do not enter the MD5 digest. Blank `bucket`, `operator` or `password` prevents calls. Leave `baseUrl`, `timeout` and `maxResponseBytes` blank for their displayed defaults.

HTTP 429, server errors and transport failures retry. A definite HTTP rejection selects optional `providerRejected`; malformed, negative, or oversized usage text selects optional `invalidResponse`. The example leaves optional branches unwired, so those outcomes fail the Flow.

The runnable example is in [`go/examples/usage-read`](go/examples/usage-read). Provider tests use a local HTTP server and a fixed signing vector. Real UPYUN operator authorization and live service usage remain unverified without a user-controlled account.
