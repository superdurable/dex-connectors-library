# Miyang Model Catalog

`ListModels` reads model IDs from the [official Miyang Gateway GET /api/v1/models API](https://miyang.cn/model/docs) over HTTPS with Bearer authentication. The [official terms](https://miyang.cn/en/terms) identify 米羊科技（上海）有限公司 as the Chinese service operator. Output contains the provider model count and at most the first 50 IDs; inference, model metadata, and account data are excluded.

Sign in at [Miyang Gateway](https://miyang.cn/model), open API Keys in the console, and create or copy a secret `miyang-` key into Dex Web Connections. The model-list endpoint is public, but a key can reveal internal or whitelisted models available to that account. The provider notes that `miyang/*` models are for its Alice client; use models marked `general` for other apps. Leave timeout and maxResponseBytes blank for their displayed defaults.

The [runnable Dex example](go/examples/list-models/) uses a synthetic mock. Live account authorization and current model visibility have not been verified without a user-controlled provider key.
