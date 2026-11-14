# ZhuanqiCloud Model Catalog

`ListModels` reads model IDs from the [official ZhuanqiCloud GET /v1/models API](https://docs.zhuanqicloud.com/) over HTTPS with Bearer authentication. The platform identifies its Chinese operator as 深圳市专企云算科技有限公司. Output contains the provider model count and at most the first 50 IDs; inference, model metadata, and account data are excluded.

Sign in at [ZhuanqiCloud](https://zhuanqicloud.com), open 控制台 > API Key, create a secret key, and copy it into Dex Web Connections. The [official quick start](https://docs.zhuanqicloud.com/9347147m0) explains that key permissions, group selection, and channel state affect visible model IDs. A listed model is not guaranteed to support every API. Leave timeout and maxResponseBytes blank for their displayed defaults.

The [runnable Dex example](go/examples/list-models/) uses a synthetic mock. Live account authorization and current model visibility have not been verified without a user-controlled provider key.
