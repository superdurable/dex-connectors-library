# BaishanCloud CDN

The Go connector reads one domain's metadata from [BaishanCloud's CDN Domain Details API](https://cdn.portal.baishan.com/track/document/api/1/1139). `DescribeDomain` returns only domain ID, name, acceleration type, service status, and CNAME. It discards origin, cache, and access-control configuration included in the provider response.

Ask your BaishanCloud account team for a CDN API token, then enter it in the secret Dex Web connection field. The provider requires the token in an HTTPS query parameter. The connector disables redirects and never copies the token, request URL, or raw response into a Flow result, receipt, log, or returned error. The official limit is 60 domain-detail requests per minute, with remaining allowance and retry guidance in response headers.

See the [runnable Dex Flow](go/examples/domain-detail/README.md) for local mock verification. Live BaishanCloud authorization and API response have not been exercised.

The logo is the original [BaishanCloud portal SVG](https://cdn.portal.baishan.com/theme/manage/images/bsc-logo.svg).

The [Python package](python/README.md) implements the same Query with Go-recorded conformance and a [runnable Flow](python/examples/domain_detail/README.md).
