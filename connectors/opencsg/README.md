# OpenCSG AI Gateway connector

`ListGatewayModels` reads the model IDs available to an OpenCSG AI Gateway API Key. It calls the official [`GET /v1/models` endpoint](https://opencsg.com/docs/csghub/101/function/aigateway/model_access) over HTTPS with a Bearer Key. The response includes the total number of returned models and at most the first 50 IDs. Inference, model descriptions, and account data are excluded.

## Authorization

Sign in to [OpenCSG](https://hub.opencsg.com), then open account settings > API Keys for a personal Key or organization settings > API Keys for a team Key. Select 新建 API Key, enter a name, optionally set quota and expiry, and copy the `gw-sk-` secret shown at creation. Paste it into Dex Web Connections. This is an AI Gateway API Key, separate from a Git Access Token. The [official API Keys guide](https://opencsg.com/docs/profile/api_keys) describes these paths. Leave the request timeout and maximum response size blank to use the manifest defaults.

## Example

The [runnable Flow](go/examples/list-gateway-models/) starts with `{}` and completes with a typed `ListGatewayModelsOutput`. Its default mock returns one model. Run the same Flow with a user-controlled Dex Web Connection for live authorization. Live OpenCSG authorization, model visibility, and provider response variation have not been verified in this change.
