# Infini AI GenStudio connector

This connector lists models available to the authenticated tenant through GenStudio's official [model discovery API](https://docs.infini-ai.com/gen-studio/api/reference/v1-redoc.html). It calls the fixed HTTPS endpoint `GET /maas/v1/models` with a Bearer API key. Output retains only each model's ID, model type, context token limit, and output token limit. A zero limit means the model type does not use that limit or the platform has not published it. Model availability and limits can change; query again before configuring a model.

In the [Infini AI console](https://cloud.infini-ai.com/), click your avatar, then **Access Control → API Key Management**. Create or select a GenStudio API key, copy its complete `sk-` prefixed value, and enter it in the Dex Web connection. The key remains a Dex credential and is never included in Flow results, receipts, Attributes, or failures. Redirects are disabled.

The runnable [list-models Flow](go/examples/list-models/README.md) uses the generated operation-specific Step and static `infiniai` connection. Transient HTTP and transport failures retry under Dex's default one-hour retry budget; definite authentication and request rejections select the optional `providerRejected` branch. Real GenStudio authorization and model catalog contents remain to be tested with a user-controlled connection.
