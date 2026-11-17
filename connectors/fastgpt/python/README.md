# FastGPT Python connector

This `v0.1.0` package implements the read-only `getDatasetDetails` Query. It returns only the knowledge base ID, name, type, and status. Provider descriptions, model settings, and permissions are omitted.

Create a personal API Key at [FastGPT OpenAPI introduction](https://doc.fastgpt.cn/zh-CN/openapi/intro): sign in, open **账号 > API 密钥**, and create a key with read access to the knowledge base. An application chat API Key cannot read knowledge base metadata. Configure the key in Dex Web Connections as the secret `api_key`. Copy the knowledge base ID from its FastGPT URL. `baseUrl` defaults to China cloud; override it only for an approved HTTPS private deployment and keep its deployment subpath, without `/api`.

The [runnable Flow](examples/get_dataset_details/) contains the static `fastgpt` connection, Worker entrypoint, real Dex integration test, and FDG instructions. Provider authorization on a live account has not been verified.
