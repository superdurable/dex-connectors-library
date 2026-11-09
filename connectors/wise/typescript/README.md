# Wise TypeScript connector

Implements `getTransfer`, returning the transfer ID, status, and source currency without recipient or payment details. Go-recorded conformance checks exact requests, branches, retries, and outputs.

Create a Wise business personal API token under **Your Account > Connect and manage apps > API tokens > Add new token** and save it as `api_token` in Dex Web Connections. Leave `baseUrl` blank for Production or set `https://api.wise-sandbox.com/2026Q4` for Sandbox. See the [connection guide](../README.md) and [runnable example](examples/transfer-inspection/README.md). Live provider authorization remains unverified.
