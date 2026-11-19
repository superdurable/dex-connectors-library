# Tuya connector for Python

Install `dex-connector-tuya==0.1.0` after its first release. The package implements the `getDeviceDetails` Query with `new_get_device_details_step`. It returns safe metadata for one linked device, excluding local keys and status values.

Create a Dex Web Connection named `tuya`. At [Tuya IoT Platform](https://platform.tuya.com/), open Cloud > Development > My Cloud Projects > Open Project > Overview. Copy Access ID and Access Secret from Authorization Key into protected credential fields. Enable IoT Core and Authorization Token Management, link the device, and select the project Data Center from the endpoint list. The connector obtains a token and signs both requests with HMAC-SHA256; credentials never enter Flow state.

The happy branch is `found`. `notFound`, `providerRejected`, `invalidResponse`, and `defect` are optional; unwired optional outcomes fail the example Flow. HTTP 408, 429, 5xx, provider transient codes, and transport failures retry. The [runnable example](examples/device_details/README.md) includes a Worker and a real Dex integration test with a fake provider. Live Tuya authorization and responses remain unverified.

## Verification

```bash
uv sync --group dev
uv run pytest tests
uv run ruff check .
uv run mypy src
```
