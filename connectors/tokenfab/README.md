# TokenFab Model Catalog

`ListModels` reads model IDs from the [official TokenFab GET /v1/models API](https://www.tokenfab.cn/docs.html) over HTTPS with Bearer authentication. The [official privacy policy](https://www.tokenfab.cn/privacy.html) identifies 深圳象元工坊科技有限公司 as the Chinese operator. Output contains the provider model count and at most the first 50 IDs; inference, model metadata, and account data are excluded.

Sign in at the [TokenFab Console](https://www.tokenfab.cn/console), open 控制台 > API 密钥, create a `tk-` prefixed secret key, and copy it once into Dex Web Connections. The provider announced that its old `.com` API domain was retired; this connector uses `api.tokenfab.cn`. Leave timeout and maxResponseBytes blank for their displayed defaults.

The [runnable Dex example](go/examples/list-models/) uses a synthetic mock. Live account authorization and current model visibility have not been verified without a user-controlled provider key.
