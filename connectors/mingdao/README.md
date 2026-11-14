# Mingdao HAP connector

This connector lists the worksheets in one Mingdao HAP application through the [official API V3 worksheet list](https://apidoc.mingdao.com/application_v3/appkey-sign/zh-Hans/). The provider uses a POST request for this read-only query. The result includes only worksheet IDs and names; remarks and row records are discarded. API keys and provider error bodies never enter Flow output, receipts, Attributes, or logs.

For Mingdao SaaS, the endpoint is fixed at `https://api.mingdao.com/v3/app/worksheets/list`. This first connector does not support private deployment hosts. In [Mingdao HAP](https://www.mingdao.com/), an application administrator opens the application's menu → **API开发文档** → **授权管理** → **新建授权密钥**, grants only the needed worksheet metadata access, and copies AppKey and Sign into Dex Web Connections. The [official API overview](https://help.mingdao.com/api/introduction/) documents this path and permission scope. Rotate or revoke the pair from the same authorization page.

The [runnable Flow](go/examples/list-worksheets/README.md) uses the static `mingdao` connection and an operation-specific Step. Real Mingdao authorization and Dex Web form interaction remain unverified.
