# Braze Python connector

This package implements `getUserProfile` for Dex Python Flows. It requests only external ID, Braze ID, given name, family name, and email from `POST /users/export/ids`; the Go-recorded conformance scenarios verify requests, branches, retries, and outputs.

In Braze Dashboard > Settings > APIs and Identifiers > API Keys, create a REST API key with `users.export.ids` permission. Enter its secret value in Dex Web `rest_api_key`, and set `baseUrl` to the account's REST Endpoint from that page. See the [connector README](../README.md) and [runnable example](examples/profile_inspection/README.md). Real Braze authorization remains unverified.
