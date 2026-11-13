# Yuexin AI Gateway connector

`ListModels` reads IDs available to a Yuexin AI Gateway API Key. The official [Models API reference](https://app.yuexin.ai/docs/api/reference/models) documents `GET /v1/models`, Bearer authentication, and the OpenAI-compatible `object: list` / `data[].id` response. Output contains the provider model count and at most the first 50 IDs. Inference, model metadata and account data are excluded.

## Authorization

Sign in to the [Yuexin console](https://app.yuexin.ai/keys), open API Keys, create a Key, set its name and optional restrictions, and save the one-time secret into Dex Web Connections. The [official key guide](https://app.yuexin.ai/docs/api-keys) explains the exact path and options. Leave timeout and maximum response size blank for the displayed defaults.

## Example

The [runnable Flow](go/examples/list-models/) starts with `{}` and completes with a typed `ListModelsOutput`. Its default mock returns one model. Live Yuexin authorization, model visibility and provider response variation remain unverified without a user-controlled Dex Web Connection.
