# Instagram Business connector

`listMedia` reads one cursor page of media from an Instagram professional account through the Meta Graph API. It returns IDs, captions, media types, timestamps, permalinks, and the next cursor. The connector requires an Instagram Business or Creator account linked to a Facebook Page; personal accounts are unsupported.

In [Graph API Explorer](https://developers.facebook.com/tools/explorer/), choose a Meta Business app, grant `pages_show_list`, `pages_read_engagement`, and `instagram_basic`, then get a User access token. Store the token in the `instagram-business` Dex Web connection. Use `GET /me/accounts?fields=instagram_business_account` to find `igUserId`. Production access may require Meta App Review. Do not put the token in a Flow input.

The runnable [Go media listing Flow](go/examples/media-list/README.md), [Python media listing Flow](python/examples/media_list/README.md), and [TypeScript media listing Flow](typescript/examples/media-list/README.md) call the operation with `{"igUserId":"17900000000000001"}`. They support `after` from the preceding `nextCursor`. Go-recorded provider scenarios check Python and TypeScript request and outcome parity. Mock backed real Dex integration tests cover all three Flows. Live Meta authorization has not been verified.

Meta's [official Instagram API collection](https://www.postman.com/meta/instagram/documentation/6yqw8pt/instagram-api) documents the Graph API media edge and authorization prerequisites.
