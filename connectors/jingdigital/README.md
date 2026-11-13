# JINGdigital connector

JINGdigital is operated by 上海径硕网络科技有限公司. This connector reads one page of ordinary marketing tag IDs, names, and hierarchy levels. It does not return users, member profiles, or access tokens.

The [official access guide](https://s.apifox.cn/739a1394-cf66-49a7-ba16-c06ffa7a6399/doc-1781384) describes credential setup. The [token endpoint](https://s.apifox.cn/739a1394-cf66-49a7-ba16-c06ffa7a6399/api-52635744) exchanges `app_id` and `app_secret`; the [tag list endpoint](https://s.apifox.cn/739a1394-cf66-49a7-ba16-c06ffa7a6399/api-141832893) reads a page using `app_id` and the temporary `access_token` headers. The [global error codes](https://s.apifox.cn/739a1394-cf66-49a7-ba16-c06ffa7a6399/doc-1781394) distinguish rejection, unavailable service, and rate limits. The documented default limit is 50 calls per second and 100,000 daily calls per tenant per interface.

## Connection

In the JINGdigital management backend, open **开发平台 > 开发与集成**, create or select a development application, enable ordinary tag list access, and copy its Appid and AppSecret into the secret connection fields. Leave endpoint and limits blank for defaults. The connector requests a temporary token on each Query; neither token nor secret enters Flow state. Provider 429, 408, 5xx, transport failures, and documented service/rate codes retry. Other rejections take an optional branch.

Run the [tag directory example](go/examples/tag-directory/README.md). Its synthetic mock is not live JINGdigital data. Live credentials, permissions, and response compatibility remain unverified.
