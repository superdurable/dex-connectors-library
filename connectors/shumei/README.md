# Shumei

The Go connector reads the current Shumei SaaS account balance in Chinese yuan. `GetBalance` has no caller input and returns only `balanceYuan`; it does not read bills, risk events, or customer data.

Shumei's [official open API manual](https://help.ishumei.com/docs/correction/openness/) documents `POST https://api-web-bj.fengkongcloud.com/saas/balance/v1` with `accessKey` in a JSON request body. It returns `code: 1100` and `content.balance` on success. The key is provided by Shumei when account service is activated; begin at the [Shumei console](https://console.ishumei.com/) and contact your account manager if it is not available. Enter it in Dex Web's secret `access_key` field. The key is never included in a URL, Flow output, or error message.

The [runnable Dex Flow](go/examples/balance-check/README.md) shows connection setup and output. The company logo embeds the unmodified [official website logo image](https://www.ishumei.com/next/img/global/logo3.png). No live account authorization has been verified.
