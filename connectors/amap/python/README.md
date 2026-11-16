# Amap connector for Python

Install `dex-connector-amap==0.1.0` after its first release. The package
implements `geocodeAddress` from the manifest and requires
`dex-connectors-sdk>=0.12.0,<1` and `dex-python-sdk>=1.28.0`.

`new_geocode_address_step` resolves a structured Chinese address. The happy
branch is `geocoded`; `providerRejected`, `invalidResponse`, and `defect` are
optional. Empty Amap matches return `geocoded` with an empty `locations` tuple.
HTTP 408, 429, 5xx, transport failures, and Amap infocodes 10004/10014 retry.
The provider request follows the
[official Amap geocoding API](https://lbs.amap.com/api/webservice/guide/api/georegeo).

## Authorization and example

Create a Dex Web connection named `amap` with a **Web Service** Key, following
the [connector setup guide](../README.md). The Key is passed as Amap's required
`key` query parameter and resolved for every attempt; it is never included in
the operation result or error text.

The runnable [`examples/address_lookup`](examples/address_lookup/flow.py) Flow
persists the validated location candidates. After starting `dexcli dev` and
configuring the connection, run the Worker with
`uv run python -m examples.address_lookup.worker` from this package directory.
The example's integration test uses a fake Amap response through a real Dex
Worker and Client; a live Amap request still needs a user-configured Key.

## Verification

`uv run pytest tests` replays the Go-recorded provider scenarios. The real Dex
test is `uv run pytest examples/address_lookup/integration_test.py` with
`DEX_FLOW_SERVICE_ADDRESS` pointing to `dexcli dev`.
