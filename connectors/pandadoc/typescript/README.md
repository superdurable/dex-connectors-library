# PandaDoc TypeScript connector

This package implements `getDocument` for Dex TypeScript Flows. It calls the PandaDoc Public API v1 and returns only a document's ID, name, and status. Go-recorded conformance scenarios verify exact requests, branches, retries, and outputs.

Create or reveal an API key under [PandaDoc Dev Center Configuration](https://app.pandadoc.com/a/#/api-dashboard/configuration) and save it in Dex Web's secret `api_key` field. The connector sends it in the `Authorization: API-Key` header. Leave `baseUrl` blank for global accounts; set it to `https://api.pandadoc.eu/public/v1` for European accounts. See the [connector README](../README.md) and [runnable example](examples/document-inspection/README.md). Live provider authorization remains unverified.
