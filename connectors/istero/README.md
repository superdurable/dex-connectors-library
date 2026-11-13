# ISAS Data account status connector

[起零数据](https://api.istero.com/service/doc/isas-user-info), operated by [大连起零网络科技有限公司](https://www.istero.com/about), exposes a free account information API. This connector reads only the account ID and daily call counters through `GET /resource/v1/isas/mine/information`; profile fields are excluded from the Flow result.

## Connection

Sign in at [起零数据](https://api.istero.com/), open **开发者中心 > 应用管理** and copy the application Token. Open **个人中心** to find the numeric 起零ISID. Enter the Token as a secret and the ISID as an account identifier in Dex Web Connections. Leave the timeout and response size blank for their displayed defaults. The connector sends the Token only in the HTTPS Bearer header. The account call is documented as free and does not consume daily call quota.

Run the [example Flow](go/examples/account-status/) with its generated mock or a user-controlled connection. It stores only ISID, today's used calls, and today's remaining calls. Authentication, quota, malformed response, transport, and retry outcomes have separate handling. Real provider authorization and response were not verified locally.
