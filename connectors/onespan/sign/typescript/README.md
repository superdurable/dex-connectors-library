# OneSpan Sign TypeScript connector

This package implements the `getPackage` Query for Dex TypeScript Flows. It reads only a package's ID, name, and status through the OneSpan Sign REST API. Go-recorded conformance scenarios verify exact requests, branches, retries, and outputs.

Reveal the account API Key under **Admin > API Access > Authentication Settings > API Key and Client Apps > API Key > View** and save it in the Dex Web connection's secret `api_key` field. The connector sends it in the `Authorization: Basic` header. Leave `baseUrl` blank for Sandbox; select the account's `/api` root for Production. See the [connection guide](../README.md) and [runnable example](examples/package-inspection/README.md). Live provider authorization remains unverified.
