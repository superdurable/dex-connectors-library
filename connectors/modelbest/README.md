# ModelBest MiniCPM connector

`ListModels` reads model IDs available to a ModelBest API Key. The official [MiniCPM OCR Jev API guide](https://github.com/OpenBMB/MiniCPM-OCR/blob/main/docs/minicpm_ocr_jev_api_zh.md#接口与鉴权) explicitly describes `GET https://api.modelbest.cn/v1/models` with a Bearer Key and a `data` list of model IDs. Output contains the provider count and at most the first 50 IDs. Inference and other model metadata are excluded.

## Authorization

Sign in to the [ModelBest console](https://platform.modelbest.cn/console/keys), open API Keys, select Create Key, name it, and copy its secret into Dex Web Connections. The [official API guide](https://github.com/OpenBMB/MiniCPM-OCR/blob/main/docs/minicpm_ocr_jev_api_zh.md#接口与鉴权) confirms Bearer Key authentication. Leave timeout and maximum response size blank for the displayed defaults.

## Example

The [runnable Flow](go/examples/list-models/) starts with `{}` and completes with a typed `ListModelsOutput`. Its default mock returns one model. Live ModelBest authorization, model visibility and provider response variation remain unverified without a user-controlled Dex Web Connection.
