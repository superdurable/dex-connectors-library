# Productboard Python connector

This package implements the `listFeatures` Query for Dex Python Flows. It reads one Productboard REST API v2 feature page and returns only feature IDs, names, and an opaque next-page cursor. The Go-recorded conformance scenarios verify exact requests, branches, retries, and outputs.

Create a Personal Access Token in [Productboard Workspace Settings > Integrations > Public APIs > Access Token](https://developer.productboard.com/reference/api-token) and enter it in Dex Web secret `api_token`. Leave `baseUrl` blank for the public API endpoint. See the [connector README](../README.md) and [runnable example](examples/feature_discovery/README.md). Real Productboard authorization remains unverified.
