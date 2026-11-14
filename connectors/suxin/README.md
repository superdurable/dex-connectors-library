# Suxin connector

Suxin (苏心签), operated by 苏州票管家网络科技有限公司, provides electronic signature services. This connector reads the organizations accessible to an API application's default user. It returns only organization ID, name, entity type, and certification status. It excludes unified social credit codes, signer and representative details, contract contents, and bearer tokens.

The operation is based on the [current Suxin Open API documentation](https://pc.suxin365.com/open-api.html) and [company information](https://suxin365.com/about/). It exchanges `appId` and `apiKey` through `POST /api/v1/auth/token`, derives `userId` from that response, then calls `GET /api/v1/org/list?userId=...` with the temporary bearer token. The documentation lists authentication at 100 requests/minute and other APIs at 500 requests/minute. The operation performs one call of each type per attempt.

## Connection

Obtain the `appId` and `apiKey` issued for your Suxin Open API application, as specified in [section 2.1 of the API documentation](https://pc.suxin365.com/open-api.html), and copy them into the secret connection fields. No user ID is required: the provider returns it at authentication. Leave endpoint and limits blank for defaults. The production endpoint is restricted to the documented HTTPS host. Provider 429, 408, 5xx, transport failures, and documented internal error 5000 retry; business rejection branches are optional and unwired by the sample Flow. Provider messages are never copied to Flow results.

Run the [organization directory example](go/examples/organization-directory/README.md). Its synthetic mock sample is not live Suxin data. Live authorization, organization permissions, and actual provider response compatibility have not been verified with real credentials.
