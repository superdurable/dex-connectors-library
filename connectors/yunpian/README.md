# Yunpian connector

The Go `getAccountBalance` Query calls Yunpian's [account lookup API](https://www.yunpian.com/official/document/sms/zh_cn/account_get) using HTTPS POST. It returns only the numeric `balance`; the account endpoint's mobile number, email address, and contact information are never included in Flow state or output. The amount uses the account's billing units as reported by Yunpian. The Query does not send SMS or change the account.

## Connect

Sign in at [Yunpian](https://www.yunpian.com), then open **Account Settings → Subaccount Management** to find the account's API key. Copy the complete secret into `apiKey` in Dex Web Connections. Blank is invalid. Configure **System Settings → IP Whitelist** for the Worker egress address before production use. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for the displayed mainland China defaults. Accounts using Yunpian's overseas endpoint can set `baseUrl` to `https://us.yunpian.com`. The operation has no input fields.

The provider's [return codes](https://global.yunpian.com/official/document/sms/zh_CN/returnvalue_common) `-5`, `-50`, and `-51`, HTTP 429/5xx, and transport failures retry. Other definite provider codes select optional `providerRejected`; invalid or oversized account JSON selects optional `invalidResponse`. These optional branches are unwired in the example and fail the Flow when selected.

The runnable Go example is in [`go/examples/balance-read`](go/examples/balance-read). Provider tests use a local HTTP server. Real Yunpian authorization, IP whitelist setup, and live account balance remain unverified without a user-controlled key.
