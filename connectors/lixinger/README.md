# Lixinger connector

[Lixinger](https://www.lixinger.com/) (理杏仁) is operated by 北京乾谦慧科技有限公司. Its [official A-share company information API](https://www.lixinger.com/api/open-api/html-doc/cn/company) documents `POST https://open.lixinger.com/api/cn/company`, a token in the JSON body, and `code: 1` for success. This connector requests one to one hundred explicit six-digit stock codes and returns company name, code, exchange, and listing status when supplied. It does not return prices, financial metrics, or investment recommendations.

## Connection

Sign in to Lixinger, open **开放平台 > [我的Token](https://www.lixinger.com/open/api/token)**, and copy your unique API Token into the secret `token` field. Ensure the account has access to A-share company basic information. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. The connector accepts only the documented HTTPS API host. Tokens remain in the request body and are never returned in Flow results or logs.

`listStockCompanies` requires 1–100 distinct six-digit `stockCodes`; the provider returns metadata for matching codes. The optional `providerRejected`, `invalidResponse`, and `defect` branches remain unwired in the example. HTTP 429, 408, 5xx, and transport failures retry within three attempts because API access may consume a paid plan allowance. Non-success provider codes select `providerRejected`; the public operation docs do not define a full error-code taxonomy.

## Example

The [stock company directory Flow](go/examples/stock-company-directory/) is a runnable Worker with static Dex connection name `lixinger`. Its mock is synthetic and verified with a real Dex Worker and Client. Live Lixinger authorization, plan allowance, matching behavior, and provider errors remain unverified without a token. These identifiers are reference data and are not investment advice.
