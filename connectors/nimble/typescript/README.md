# Nimble TypeScript connector

This package implements `listContactIDs` and `getContact` for Dex TypeScript Flows. Both operations replay the Go reference's provider requests, branches, retries, and exact outputs through shared conformance scenarios. Results contain contact IDs and selected name and email fields; custom fields and links remain outside Flow results.

Sign in to [Nimble CRM](https://app.nimble.com/) as an administrator, open **Settings > API Token > Generate New Token**, name the token and choose Contacts scope, then copy it into Dex secret `api_key`. An administrator may need to grant API access under **Settings > Users**. See the [connector README](../README.md) and [contact ID discovery example](examples/contact-id-discovery/README.md).

Live Nimble authorization and responses have not been verified locally. Package release is independent of the Go and Python releases.
