# Meiqia Conversation Read

This connector reads limited metadata for one customer service conversation through the [official Meiqia API](https://www.meiqia.com/help/article/apiv2-0/). The [official privacy policy](https://www.meiqia.com/privacy-clause) identifies 成都美洽网络科技有限公司 as the service operator.

Sign in at [Meiqia](https://app.meiqia.com), open 设置 > 开发者 > APIs, and copy the current secret Access Token into a `meiqia` Dex Web connection. Creating a new token invalidates the previous one. Pass a positive numeric `conversationID`. The connector sends a Bearer token over HTTPS and returns only ID, start time, end time, and main channel. It excludes messages, customer data, agent identity, source URLs, and raw provider responses. Blank timeout and maxResponseBytes use their displayed defaults.

The [runnable Dex example](go/examples/get-conversation/) uses a synthetic mock and does not verify live Access Token permissions or conversation data.
