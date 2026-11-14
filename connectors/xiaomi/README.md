# Xiaomi MiMo connector

`ListModels` reads IDs from Xiaomi MiMo's official [GET /v1/models endpoint](https://mimo.mi.com/docs/zh-CN/api/model/list-models) over HTTPS with Bearer authentication. Output contains the provider model count and at most the first 50 IDs. Inference, model metadata and account data are excluded.

## Authorization

Sign in to the [MiMo API platform](https://platform.xiaomimimo.com) with a Xiaomi account, open Console > API Keys, create a pay-as-you-go API Key and copy its `sk-` secret into Dex Web Connections. The [official API integration guide](https://mimo.mi.com/docs/zh-CN/quick-start/faq/api-integration) distinguishes this Key from Token Plan `tp-` Keys, which use a separate base URL. Leave timeout and maximum response size blank for the displayed defaults.

## Example

The [runnable Flow](go/examples/list-models/) starts with `{}` and completes with a typed `ListModelsOutput`. Its default mock returns one model. Live MiMo authorization, model visibility and response variation remain unverified without a user-controlled Dex Web Connection.
