# Lucid Python connector

This package implements `listDocuments` for Dex Python Flows. It searches one page of accessible, non-trashed Lucid documents and returns only IDs, titles, and the next opaque page token. Go-recorded conformance scenarios verify requests, branches, retries, and outputs.

Create a personal API key in [Lucid Developer Portal > API Keys](https://lucid.app/developer) with `DocumentReadonly` grant and enter it in Dex Web secret `api_key`. Leave `baseUrl` blank for the public API endpoint. See the [connector README](../README.md) and [runnable example](examples/document_discovery/README.md). Real Lucid authorization remains unverified.
