# GuGuData text segmentation

This Go connector calls the [GuGuData Chinese text segmentation API](https://www.gugudata.com/api/details/segment) to split up to 2000 UTF-16 code units into words in their original order. It returns the provider's comma-joined `SegmentaryText` field as `segmentedText`.

Sign in at the [developer center AppKey page](https://www.gugudata.com/developers/portal/appkeys), enable the segmentation product, and store the AppKey in the Dex Web `gugudata` connection. The key is sent over HTTPS in the recommended `X-GUGUDATA-APPKEY` header, not in the URL; errors and Flow results omit it. Queries may consume quota. The [official OpenAPI contract](https://www.gugudata.com/openapi/gugudata.openapi.3.1.json) defines the query parameter, response, and status codes.

See the [runnable Dex Flow](go/examples/segment-text/README.md) for local mock verification. A real GuGuData account and live API call have not been exercised.
