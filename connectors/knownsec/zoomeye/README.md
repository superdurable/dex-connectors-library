# Knownsec ZoomEye account quota

The Go connector reads the connected ZoomEye account's subscription plan and remaining general and ZoomEye points. The current [ZoomEye agent documentation](https://www.zoomeye.ai/agent/doc) identifies `POST /v2/userinfo`, HTTPS, the `API-KEY` header, and success code 60000. The [official ZoomEye API v2 reference](https://github.com/zoomeye-ai/ZoomEye-python/blob/master/api.md) gives the response schema, including `data.subscription.plan`, `points`, and `zoomeye_points` as strings.

The provider response also includes username, email, and phone. The connector decodes only the subscription object and never returns personal account fields, raw response bodies, or the API key. HTTP 429/5xx and transport failures retry; non-success provider codes and permission failures are terminal.

See the [runnable Dex Flow](go/examples/account-quota/README.md). Mock tests do not verify live authorization, subscription availability, or provider response behavior.
