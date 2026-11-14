# Huawei Cloud IAM connector

This connector runs the read-only [List Projects Accessible to an IAM User](https://support.huaweicloud.com/intl/en-us/api-iam/iam_06_0003.html) query. It first obtains a domain-scoped IAM token using the [official password authentication API](https://support.huaweicloud.com/intl/en-us/api-iam/iam_30_0001.html), then calls `GET /v3/auth/projects` on the documented global endpoint. The token and password remain in memory for the provider call and never enter Flow input, output, receipts, Attributes, or logs.

Create an IAM user with a password. A HUAWEI ID password cannot directly obtain this token. In Dex Web Connections, supply the account name, IAM username, and IAM password from the Huawei Cloud console. The IAM API lists only projects where that user has accessible resources; an empty list is a valid result. Credential errors select `providerRejected`; rate limits and temporary service errors retry. This connector does not cache tokens, so each attempt requests a fresh token.

The runnable [list-projects example](go/examples/list-projects/README.md) demonstrates the generated operation-specific Step and static `huaweicloud` connection. Real Huawei Cloud authorization remains to be tested with a user-controlled Dex Web Connection.
