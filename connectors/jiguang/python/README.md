# Jiguang JPush connector for Python

Install `dex-connector-jiguang==0.1.0` after its first release. The package implements the `listApplicationTags` Query with `new_list_application_tags_step`. It returns the configured JPush application's tag names without device identifiers or push payloads.

Create a Dex Web Connection named `jiguang`. At [Jiguang Developer Platform](https://www.jiguang.cn), open Developer Platform > Application Management > App Settings and copy the application's AppKey and paired Master Secret. Save the Master Secret in Dex's protected credential field. The connector sends both only through HTTPS Basic authentication and does not store them in a Flow result. Leave the endpoint and limits blank to use displayed defaults.

The happy branch is `listed`. `providerRejected`, `invalidResponse`, and `defect` are optional; unwired optional outcomes fail the example Flow. HTTP 408, 429, 5xx, and transport failures retry, with `Retry-After` or `X-Rate-Limit-Reset` delay where supplied. The [runnable example](examples/tag_read/README.md) includes a Worker and a real Dex integration test with a fake provider. Live JPush authorization and responses remain unverified.

## Verification

```bash
uv sync --group dev
uv run pytest tests
uv run ruff check .
uv run mypy src
```
