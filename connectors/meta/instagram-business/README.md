# Instagram Business connector

`getAccount` reads a professional account's ID, username, and media count. `getMedia` reads one media object's ID, caption, type, timestamp, and permalink. `listMedia` reads one cursor page of those media fields. The connector requires an Instagram Business or Creator account linked to a Facebook Page; personal accounts are unsupported.

In [Graph API Explorer](https://developers.facebook.com/tools/explorer/), choose a Meta Business app, grant `pages_show_list`, `pages_read_engagement`, and `instagram_basic`, then get a User access token. Store the token in the `instagram-business` Dex Web connection. Use `GET /me/accounts?fields=instagram_business_account` to find `igUserId`. Production access may require Meta App Review. Do not put the token in a Flow input.

The runnable [Go media and account Flows](go/examples/media-list/README.md) cover `listMedia` and `getAccount`. The [Python media listing Flow](python/examples/media_list/README.md) and [TypeScript media listing Flow](typescript/examples/media-list/README.md) continue to support `listMedia`. Go-recorded provider scenarios cover the Go operations and check Python and TypeScript parity for the operation those packages implement. Mock backed real Dex integration tests cover the Go Flows. Live Meta authorization has not been verified.

Meta's [IG User reference](https://developers.facebook.com/docs/instagram-platform/instagram-graph-api/reference/ig-user/v21.0/) and [Instagram Media reference](https://developers.facebook.com/docs/instagram-platform/reference/instagram-media/v21.0/) document the requested fields and authorization prerequisites.
