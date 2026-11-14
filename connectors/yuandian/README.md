# Yuandian Open Platform

This connector reads selected company registration facts by unified social credit code using the [official company detail API](https://open.chineselaw.com/api-docs/rh_company_detail.html). Beijing Huayu Yuandian Information Services operates the platform, as stated in its [service agreement](https://www.chineselaw.com/ServiceContract.pdf).

Sign in at [Yuandian Open Platform](https://open.chineselaw.com), open 个人中心 > API Key 管理, create or select a key, and copy it into a `yuandian` Dex Web connection. The key is secret and sent in `X-API-Key`. Check access to the company detail API and account quota. Supply an 18-character uppercase `creditCode`. The connector filters the provider response to company ID, name, credit code, registration status, industry, and company type; it discards personal details and litigation records. Blank timeout and maxResponseBytes use their displayed defaults. Requests may consume quota, so transient failures have a limited retry budget.

The [runnable Dex example](go/examples/get-company-profile/) uses the static `yuandian` connection and includes a provider mock for local verification. Its mock does not verify a real Yuandian API Key or live company data.
