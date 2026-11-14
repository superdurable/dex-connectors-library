# Qingflow connector

This connector calls the current [Qingflow OpenAPI application list](https://help-center.qingflow.com/docs/outline/jhzcl2q96h/), `GET https://api.qingflow.com/app`, and returns only `appKey` and `appName`. It does not read application records, form contents, or members.

In the target workspace, open [轻流](https://qingflow.com/) **轻商城 > 插件中心 > 第三方连接 > OPEN API > 配置** and copy the secret super-admin `accessToken` into Dex Web Connections. The [official API documentation](https://help-center.qingflow.com/docs/outline/jhzcl2q96h/) says permission-group tokens cannot call this list endpoint. The [plugin guide](https://help-center.qingflow.com/docs/outline/2iztdeizlf/) lists OPEN API under the Enterprise version. The token is sent only in the HTTPS `accessToken` header, never in Flow input.

The public-cloud endpoint is fixed; DingTalk and private Qingflow deployments have different hosts. Run the [example Flow](go/examples/list-applications/) with the generated mock or a user-controlled connection. Real Qingflow authorization and plan entitlement remain to be tested with an account.
