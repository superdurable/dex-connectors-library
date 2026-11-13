# SenseTime SenseNova connector

`ListModels` reads model IDs from SenseTime SenseNova's official [GET /v1/llm/models endpoint](https://console.sensecore.cn/micro/help/docs/model-as-a-service/nova/overview/Models/GetModelList/) over HTTPS with Bearer API Key authentication. Output contains the provider model count and at most the first 50 IDs. Inference, model permissions and account data are excluded.

## Authorization

Sign in to the [SenseCore console](https://console.sensecore.cn), activate SenseNova, then open ModelStudio Management Center > API Key Management and create a Key. Copy its secret into Dex Web Connections. The [official authentication guide](https://console.sensecore.cn/micro/help/docs/model-as-a-service/nova/overview/Authorization/) says a directly supplied API Key may replace a generated JWT in the Bearer header. Leave timeout and maximum response size blank for the displayed defaults.

## Example

The [runnable Flow](go/examples/list-models/) starts with `{}` and completes with a typed `ListModelsOutput`. Its default mock returns one model. Live SenseNova authorization, model visibility and provider response variation remain unverified without a user-controlled Dex Web Connection.
