# Recurly Python connector

This package implements `getAccount`. It returns account ID and code without contact, billing, or payment details. Go-recorded conformance checks exact requests, branches, retries, and outputs.

Create a secret Private API Key under **Integrations > API Credentials** in Recurly. Save the unencoded key as `api_key` in Dex Web Connections. Leave `baseUrl` blank for the fixed v3 endpoint. See the [connection guide](../README.md) and [runnable example](examples/account_inspection/README.md). Live provider authorization remains unverified.
