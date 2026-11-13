# Wangdian connector

Wangdian (旺店通) is operated by 北京掌上先机网络科技有限公司. This connector reads one bounded page of enabled ERP shop identifiers, names, platform IDs, and authorization states. It excludes platform accounts, emails, contacts, addresses, refresh tokens, and other shop details.

The [official shop API](https://open.wangdian.cn/Y/open/apidoc/doc?path=shop_Y) defines `POST https://openapi.huice.com/openapi/shop.php` with form parameters and a JSON response. The [protocol guide](https://open.wangdian.cn/open/guide?path=guide_jkgf) requires form POST and documents the 60 calls/minute, 3 concurrent requests limit for `shop.php`. The [signing guide](https://open.wangdian.cn/open/guide?path=guide_signsf) defines the MD5 signature and a published fixed vector. The [error guide](https://open.wangdian.cn/open/guide?path=guide_cwmsm) distinguishes throttling, temporary failures, quota exhaustion, and access rejection.

## Connection

Register at the [Wangdian Open Platform](https://open.wangdian.cn/), wait for approval, then open **自助对接 > 应用管理** to create or select an appkey with `shop.php` access and obtain the paired appsecret. Obtain the seller account `sid` from the ERP buyer administrator. Store all three as secret connection fields. The appsecret is used only to sign requests and never transmitted. Leave endpoint and limits blank for defaults. `page` is zero based and `pageSize` is 1–100, default 30.

Run the [shop directory example](go/examples/shop-directory/README.md). Its synthetic mock is not live Wangdian data. Live credentials, permissions, actual shop list compatibility, and production rate limits have not been verified.
