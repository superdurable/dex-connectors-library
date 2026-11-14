# StepFun connector

This connector implements the read-only [model list API](https://platform.stepfun.com/docs/zh/api-reference/models/list). It sends an API key as a Bearer header to the fixed `https://api.stepfun.com/v1/models` endpoint and returns the documented model ID, Unix creation time, and owner. It never places the key in Flow input, output, receipts, Attributes, or logs. An empty list is valid.

Create a personal API key from [Interface Keys](https://platform.stepfun.com/interface-key), or follow the [organization project key guide](https://platform.stepfun.com/docs/zh/guides/organization/api-keys). Enter the full key in Dex Web Connections. Rate limits and temporary service errors retry; invalid keys select `providerRejected`.

The runnable [list-models example](go/examples/list-models/README.md) demonstrates the generated operation-specific Step and static `stepfun` connection. Real StepFun authorization remains to be tested with a user-controlled Dex Web Connection.
