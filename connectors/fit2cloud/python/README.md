# FIT2CLOUD Python connector

`dex-connector-fit2cloud` provides the `listPosts` Query for one page of Halo CMS article metadata. It returns names, titles, slugs, publication state, visibility, and the next page number without article bodies or author details. The personal access token stays in connection credentials.

## Connection

On your HTTPS Halo site, open **personal center → Personal Access Tokens**, create a token with article read permission and an expiry, then copy its `pat_` value into the secret `api_key` field in Dex Connections. Blank prevents requests. Set `baseUrl` to that site's HTTPS origin, without `/apis`; blank prevents connection. Leave `timeout` and `maxResponseBytes` blank for the displayed defaults. See the [Halo user center guide](https://docs.halo.run/guide/use/user-center).

`page` is one-based and `pageSize` is 1–100; zero selects page 1 and 20 items. Optional `providerRejected`, `invalidResponse`, and `defect` outcomes fail the Flow unless the application wires a reaction. Temporary HTTP and transport failures retry under the Step policy. See `examples/post_catalog/` for a runnable Flow, Worker, real Dex integration test, and strict FDG instructions. `uv run pytest tests` replays Go-recorded provider scenarios. Live Halo authorization remains unverified.
