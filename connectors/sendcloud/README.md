# SendCloud (China) account balance

This connector reads [SendCloud user information](https://www.sendcloud.net/doc/email_v2/user_info/) for the configured API_USER. The operation returns only the provider's available balance, total balance, daily request quota, and today's used quota. The provider also returns email, phone, username, and verification details; the connector discards those fields. Decimal balances are preserved as strings, and no currency is inferred because the API reference does not specify one.

SendCloud's [API rules](https://www.sendcloud.net/doc/email_v2/) define `result` and `statusCode`; code `50000` is a rate limit. Both API_USER and API_KEY are Dex credentials sent over HTTPS using the provider's documented query parameters. Errors and Flow outputs omit them. The [runnable Dex Flow](go/examples/account-balance/README.md) demonstrates the static connection and mock-backed verification.

This is the Chinese email platform operated by [武汉闪达科技有限公司](https://www.sendcloud.net/aboutUs/), distinct from the European shipping service with a similar name. The logo comes from SendCloud's official site.
