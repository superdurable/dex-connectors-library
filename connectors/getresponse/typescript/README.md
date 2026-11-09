# GetResponse TypeScript connector

This package implements `listContacts` and `getContact` for Dex TypeScript Flows. Both operations replay the Go reference's provider requests, branches, retries, and exact outputs through shared conformance scenarios. Embedded relationships and custom field values are excluded from Flow results.

Sign in at [GetResponse Integrations and API](https://app.getresponse.com/api), open Integrations and API > API, and generate or copy an API Key into the Dex Web connection secret `api_key`. Leave `baseUrl` blank for the displayed SMB API default. The package sends the key only in the `X-Auth-Token` header with the required `api-key` prefix. The [connector README](../README.md) describes operation inputs and defaults; [contact inspection](examples/contact-inspection/README.md) is the runnable example.

Live GetResponse authorization and responses have not been verified locally. Package release is independent of the Go and Python releases.
