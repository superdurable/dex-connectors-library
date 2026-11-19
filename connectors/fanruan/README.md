# FanRuan Jiandaoyun connector

This connector reads one page of applications allowed by a Jiandaoyun API Key through the [official user app list API](https://hc.finecloud.com/app/open/18538). The provider uses POST for this read-only query. Each result contains only the application ID and name; it does not read forms, records, or personal data. A full page suggests `nextSkip`; call again with that offset to check for more apps.

The endpoint is fixed at `https://api.jiandaoyun.com/api/v5/app/list`. Per the [development guide](https://hc.finecloud.com/app/open/11261), every call uses HTTPS and `Authorization: Bearer <API Key>`. In [Jiandaoyun](https://www.jiandaoyun.com/) open **开放平台 > 密钥管理 > API Key**, create a key, and restrict its application and interface scope. Reveal the key with the eye icon and copy it into Dex Web Connections without the Bearer prefix. Disable or delete it on the same page when revoking access.

The [runnable Flow](go/examples/list-apps-page/README.md) uses the static `fanruan` connection. Real API Key authorization and Dex Web form interaction remain unverified.

The [Python v0.1.0 package](python/README.md) implements the same `listAppsPage` Query with Go-recorded request and outcome conformance. Its [runnable application directory Flow](python/examples/app_directory/README.md) uses the static `fanruan` connection, a real Dex Worker and Client, and an inert fake provider for retry and result verification. Live provider authorization remains unverified without a user-controlled key.
