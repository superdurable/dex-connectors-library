# BaishanCloud connector for Python

Install `dex-connector-baishancloud==0.1.0` after its first release. The package implements the `describeDomain` Query with `new_describe_domain_step`. It reads one CDN domain from BaishanCloud's [Domain Details API](https://cdn.portal.baishan.com/track/document/api/1/1139) and returns only ID, name, service type, status, and CNAME. Origin and cache configuration in the provider response are discarded.

Create a Dex Web Connection named `baishancloud`. Request a CDN API token from your BaishanCloud account team through the [CDN portal](https://cdn.portal.baishan.com/track/document/api/1/1139); save it in the secret `token` field. The provider requires the token in an HTTPS query parameter. The connector disables redirects and never returns the token, raw URL, or raw response in a Flow result or error. The provider's published limit is 60 domain-detail requests per minute.

The happy branch is `found`. `notFound`, `providerRejected`, `invalidResponse`, and `defect` are optional and fail the example Flow when selected. HTTP 408, 429, 5xx, and transport failures retry; `Retry-After` and `X-Retry-After` supply the delay. The [runnable example](examples/domain_detail/README.md) includes a Worker and a real Dex integration test using a fake provider. Live BaishanCloud authorization and responses remain unverified.

## Verification

```bash
uv sync --group dev
uv run pytest tests
uv run ruff check .
uv run mypy src
```
