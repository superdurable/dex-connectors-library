# West.cn (西部数码) connector

[West.cn](https://www.west.cn/aboutus/contact.asp) is operated by 成都西维数码科技有限公司. This Go connector calls the official [HTTPS v2 domain-list API](https://api.west.cn/CustomerCenter/doc/domain_v2.html) to read one page of domain names and expiration dates from an approved reseller account. It does not request WHOIS contacts, management passwords, or other sensitive domain information.

## Connection

The [official authentication guide](https://api.west.cn/CustomerCenter/doc/apiv2.html) limits API access to approved reseller partners. Sign in at [West.cn](https://www.west.cn/), find the registered username in your account profile, and open **代理商管理 > API接口配置** to view or set the API password. Enter both values in the Dex connection; the password is secret. Dex derives a 10-minute MD5 token from the username, password, and current millisecond timestamp for each HTTPS request. The provider requires the token as a query parameter. Leave base URL, timeout, and response limit blank for displayed defaults. Rotate a compromised API password in the reseller settings.

## Go

The [domain catalog Flow](go/examples/domain-catalog/) is a runnable Worker with static Dex Web connection name `west`. Input `page` and `limit` default to 1 and 20; `listed` completes the Flow. Optional branches fail unless an application wires a business response. Transient failures retry safely. The local mock integration uses synthetic domain data and a real Dex Worker and Client. Live reseller authorization and domain values remain unverified without a user-controlled connection.
