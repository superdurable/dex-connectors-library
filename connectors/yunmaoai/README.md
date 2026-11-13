# YunMao AI connector

`ListModels` reads IDs visible to a 云猫 API Key. The [official developer guide](https://yunmaoai.com/docs.html) documents `GET https://api.yunmaoai.com/v1/models`, Bearer authentication, and OpenAI compatible API behavior. Output contains the provider model count and at most the first 50 IDs. The provider says the list may include models that have not been verified callable; inference and model metadata are excluded.

## Authorization

Start at the [云猫 registration page](https://api.yunmaoai.com/sign-up), then open the console's 令牌 or API 密钥 page, create a Key, and copy its secret into Dex Web Connections. The [official guide](https://yunmaoai.com/docs.html) states the Bearer header format. Leave timeout and maximum response size blank for the displayed defaults.

## Example

The [runnable Flow](go/examples/list-models/) starts with `{}` and completes with a typed `ListModelsOutput`. Its default mock returns one model. Live 云猫 authorization, model visibility and response variation remain unverified without a user-controlled Dex Web Connection.
