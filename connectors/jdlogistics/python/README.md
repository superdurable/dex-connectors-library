# JD Logistics connector for Python

Install `dex-connector-jdlogistics==0.1.0` after its first release. The package implements the `queryWaybillTrace` Query with `new_query_waybill_trace_step`. It reads scans for one authorized B2C merchant waybill. Output retains only the waybill number, state, category, and scan time; provider phone numbers, addresses, names, and remarks are discarded.

Create a Dex Web Connection named `jdlogistics`. At [JD Logistics Open Platform](https://open.jdl.com), sign up as a merchant, create an application under 控制台 > 开发管理 > 应用管理, obtain its AppKey and AppSecret, authorize the merchant through the documented simplified mode to obtain an access token, and obtain the customer code from qualification details. Save AppSecret and the access token as Dex secrets. The connector signs each HTTPS request and never stores these values in Flow state. Leave the endpoint and limits blank for displayed defaults.

The happy branch is `found`. `providerRejected`, `invalidResponse`, and `defect` are optional and fail the example Flow when selected. HTTP 408, 429, 5xx, transient provider codes, and transport failures retry. The [runnable example](examples/waybill_trace/README.md) includes a Worker and a real Dex integration test with a fake provider. Live JD Logistics authorization and responses remain unverified.

## Verification

```bash
uv sync --group dev
uv run pytest tests
uv run ruff check .
uv run mypy src
```
