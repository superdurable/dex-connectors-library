# Chukou1 Python connector

`dex-connector-chukou1` implements the Go-compatible `listWarehouses` Query. It reads up to 200 authorized warehouse codes, names, country names, and two-letter country codes from `GET /v1/warehouses`. The operation has no input fields. A provider refusal selects `providerRejected`, malformed or oversized data selects `invalidResponse`, invalid local credentials select `defect`, and 408, 429, 5xx, and transport failures retry. Optional branches fail the Flow when unwired.

Configure the static `chukou1` connection in Dex Web. Follow the [authorization guide](../README.md) to get a fixed access token from **Account > Customer Authorization Management > Authorize > View Token** in Chukou1. Store it only in `access_token`, without `Bearer`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for the documented defaults. There are no operation fields to configure.

The checked-in [warehouse list example](examples/warehouse_list/README.md) runs one Flow with a real Dex Worker and Client. `uv run pytest tests` replays the Go-recorded provider scenarios. No live Chukou1 account was used in local verification.
