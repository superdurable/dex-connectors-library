# Kimi API connector

`ListModels` reads model IDs available to a Kimi API Key. It calls the official [GET /v1/models endpoint](https://platform.kimi.com/docs/api/list-models.md) over HTTPS with Bearer authentication. Output contains the provider model count and at most the first 50 IDs. Inference, model metadata and account data are excluded.

## Authorization

Sign in to the [Kimi API platform](https://platform.kimi.com), open [Console > API Keys](https://platform.kimi.com/console/api-keys), create a Key, and copy its secret value into Dex Web Connections. The [official key guide](https://platform.kimi.com/docs/get-api-key.md) describes this path. Use the China platform Key; international `platform.kimi.ai` Keys are separate. Leave timeout and maximum response size blank for the displayed defaults.

## Example

The [runnable Flow](go/examples/list-models/) starts with `{}` and completes with a typed `ListModelsOutput`. Its default mock returns one model. Live Kimi authorization, model visibility, and provider response variation remain unverified without a user-controlled Dex Web Connection.
