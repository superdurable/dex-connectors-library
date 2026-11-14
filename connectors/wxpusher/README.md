# WxPusher subscriber count

`GetSubscriberCount` reads `data.total` from WxPusher's [official user-list V2 API](https://wxpusher.zjiecode.com/docs/). It asks for page 1 with page size 1, discards the user record, and returns only the total number of subscription records. One person may subscribe to multiple applications or topics and therefore appear more than once. The official documentation identifies WxPusher as a service of 四川思明今创科技有限公司.

The application token is sent in the provider's documented HTTPS query parameter. The client does not follow redirects; errors, results, and receipts never include the token or URL. HTTP 429, 408, 5xx, and transport failures retry. Definite rejections take an optional branch. Responses are bounded to 64 KiB.

The [runnable Dex Flow](go/examples/subscriber-count/README.md) uses a static connection, typed result, strict FDG and mock-backed real Dex integration. Live application authorization and production responses remain unverified. The logo comes from the [official WxPusher GitHub organization](https://github.com/wxpusher).
