# Bilibili connector for Python

Install `dex-connector-bilibili==0.1.0` after its first release. The package implements the `getAuthorizedUser` Query with `new_get_authorized_user_step`. It returns the authorized account's application-scoped `openID` and nickname from Bilibili's [official account endpoint](https://open.bilibili.com/doc/4/feb66f99-7d87-c206-00e7-d84164cd701c). It omits the avatar URL.

Create a Dex Web Connection named `bilibili`. At the [Open Platform](https://open.bilibili.com), register an application, obtain its `client_id` and `app_secret`, request `USER_INFO`, and have the user authorize it. Save the client ID and secret plus the resulting OAuth2 access token in the connection. The app owner must refresh expiring tokens. The connector signs each GET request using Bilibili's [HMAC-SHA256 signature contract](https://open.bilibili.com/doc/4/8673959e-f7bb-56e6-6e68-d225f971b81b), a new nonce, and a current timestamp. Secrets, signatures, and raw responses do not enter Flow output.

The happy branch is `found`. `providerRejected`, `invalidResponse`, and `defect` are optional and fail the example Flow when selected. HTTP 408, 429, 5xx, and documented busy or rate-limit codes retry. The [runnable example](examples/authorized_user/README.md) includes a Worker and a real Dex integration test using a fake provider. Live `USER_INFO` approval, user authorization, and production account data remain unverified.

## Verification

```bash
uv sync --group dev
uv run pytest tests
uv run ruff check .
uv run mypy src
```
