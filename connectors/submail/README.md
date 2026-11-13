# SUBMAIL SMS Balance

The Go `getSMSBalance` Query reads general and transactional SMS credits through SUBMAIL's documented [Balance/SMS API](https://www.mysubmail.com/documents/AIcGd4). It POSTs to `/balance/sms.json` over HTTPS with `appid` and the paired AppKey as `signature`, using SUBMAIL's documented normal authorization mode. It never sends messages. The AppKey stays in the Dex credential and is omitted from Flow input, output, and logs.

## Connect

Sign in at the [SUBMAIL SMS application page](https://www.mysubmail.com/chs/sms/apps), then open **Console > SMS > Create/Manage AppID**. Create or select an enabled SMS application. Copy its nonsecret AppID into `appId` and its secret AppKey into `appKey` in Dex Web Connections. Blank values prevent calls. Rotate an exposed AppKey from the same application page. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for the displayed defaults.

The Query returns the provider's decimal strings as `generalCredits` and `transactionalCredits`. It retries transport failures, HTTP 429, and temporary 5xx responses; definite provider errors select optional `providerRejected`, and malformed or oversized responses select optional `invalidResponse`. The example leaves optional branches unwired, so those outcomes fail the Flow.

Run the [SMS Balance example](go/examples/sms-balance) with its static `submail` connection. Provider tests use a local HTTP server. Live SUBMAIL account authorization and balance values remain unverified without a user-controlled SMS application.
