# IPIP connector

This connector calls IPIP's official [v2 online IP query API](https://www.ipip.net/document/api_v2.html), `GET https://ipapi.ipip.net/v2/query/{IP}?token=...`, and returns only the default country, region, city, and two-letter country-code fields. It ignores all custom-only fields, coordinates, domains, and provider messages. IPIP is operated by [Beijing Tiantexin Technology Co., Ltd.](https://www.ipip.net/contact.html).

Sign in or register at [IPIP](https://user.ipip.net/) and obtain a token for your online query service. The public API document does not identify the exact account page where the token appears; [IPIP sales](https://www.ipip.net/contact.html) can provide account-specific instructions. Save the token in Dex Web Connections. The provider requires it in the HTTPS query string, so the connector disables redirects and never returns or logs the URL, token, or raw error message.

Run the [example Flow](go/examples/lookup-ip-location/) with its generated mock or a user-controlled connection. An IP address can identify a device or person; pass only addresses you are authorized to query. Live pricing and quota depend on your IPIP agreement and are not stated in the v2 API document.
