# FXiaoke CRM connector

This connector lists [CRM object metadata](https://developer.fxiaoke.com/openapi_v2/common/system/object-describe/list.html) through FXiaoke OpenAPI. It returns only object API names, display names, and active flags. It does not read customer, contact, or employee records. The provider's list endpoint uses POST as a read operation.

The connector exchanges the enterprise app's AppId, APPSecret, and permanent authorization code for a short-lived access token. It caches the token in memory, serializes exchanges, and refreshes before expiry as required by the [client credentials guide](https://developer.fxiaoke.com/openapi_v2/start/auth/app-info.html). Neither credentials nor the token enter Flow data, receipts, or logs. The employee ID supplies the CRM permission context. FXiaoke may require an OpenAPI quota and an outbound IP allowlist.

Set up an enterprise app in [FXiaoke](https://www.fxiaoke.com/) under **管理后台 > 应用列表 > 新建企业自建应用**. Enable **开发模式**, copy the AppId, APPSecret, and 永久授权码, restrict **CRM接口查询权限设置**, and add the Dex Worker's outbound IP to **白名单ip**. Select a service employee in **CRM > 人员** and copy its **账号信息 > 员工ID**. Choose the [OpenAPI host](https://developer.fxiaoke.com/openapi_v2/start/guide/cloud.html) matching your login cloud. The [runnable example](go/examples/list-object-metadata/README.md) uses a static `fxiaoke` connection.

Real FXiaoke authorization and Dex Web form interaction remain unverified; provider tests use a local fake.
