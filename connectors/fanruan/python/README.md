# FanRuan Jiandaoyun Python connector

`dex-connector-fanruan` implements the read-only `listAppsPage` Query for Dex Flows. It follows the [official user application list API](https://hc.finecloud.com/app/open/18538): `POST https://api.jiandaoyun.com/api/v5/app/list` with JSON `limit` and `skip`. The result contains only application ID and name. A full page suggests `nextSkip`; use that offset to ask for another page.

## Connection

Create a static `fanruan` connection in Dex Web. At [Jiandaoyun](https://www.jiandaoyun.com/), open **开放平台 > 密钥管理 > API Key**, create a key, grant only the needed applications and interface scope, reveal it with the eye icon, and copy its full value into secret `api_key` without the `Bearer` prefix. Blank prevents requests. Disable or delete it from the same page to revoke access. Leave `timeout` and `maxResponseBytes` blank for displayed defaults; Python timeout is measured in seconds and response bound in bytes. The HTTPS endpoint is fixed; no tenant ID or URL is entered.

The Flow input `skip` omits that many earlier applications; zero starts at the beginning. `limit` is 1–100; zero uses 100. The connector sends `Authorization: Bearer <API Key>` with redirects disabled. It returns no forms, records, or personal data. HTTP 429/5xx, provider code 8303, and transport failures retry. Definite rejections select optional `providerRejected`; malformed or oversized responses select `invalidResponse`; invalid local pagination or credentials select `defect`.

The [runnable application directory Flow](examples/app_directory/README.md) records the restricted page in a Dex Attribute. Its integration test uses an inert key and fake provider with a real Dex Worker and Client. Live Jiandaoyun authorization and application scopes remain unverified without a user-controlled key.
